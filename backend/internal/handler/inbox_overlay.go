package handler

import (
	"cmp"
	"context"
	"errors"
	"log"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
	"github.com/redis/go-redis/v9"
)

// This file holds the parts of the bank-line inbox that do not come from
// Claude: the factuurbetaling payload, the open-post matcher, the mail-folder
// document annotation and the overlay that puts both on top of the cached
// classification. Everything except the two fetch helpers is pure.

// Soort codes of the two payment mutation types. A factuurbetaling settles an
// open invoice through relatieId + factuur on its line: e-boekhouden looks the
// invoice up itself, so the line carries no tegenRekening and no BTW split,
// and the mutation has no inEx. SubmitFull books the same shape in step 3.
const (
	soortFactuurbetalingOntvangen = 3 // payment received on a sales invoice
	soortFactuurbetalingVerstuurd = 4 // payment sent on a purchase invoice
)

// Soort names as the frontend and the learned_classifications table use them.
const (
	soortNameFactuurbetalingOntvangen = "FactuurbetalingOntvangen"
	soortNameFactuurbetalingVerstuurd = "FactuurbetalingVerstuurd"
)

// isFactuurbetaling reports whether soort is one of the two payment types.
func isFactuurbetaling(soort int) bool {
	return soort == soortFactuurbetalingOntvangen || soort == soortFactuurbetalingVerstuurd
}

// inboxBatchItem is one line of POST /inbox/process-batch.
type inboxBatchItem struct {
	ID            int     `json:"id"`            // bank line ID (importId)
	GrootboekId   int     `json:"grootboekId"`   // bank account internal ID
	Soort         int     `json:"soort"`         // mutation type code
	Grootboekcode string  `json:"grootboekcode"` // tegenrekening code (from AI)
	BTWCode       string  `json:"btwCode"`
	Omschrijving  string  `json:"omschrijving"`
	Bedrag        float64 `json:"bedrag"`
	RelatieId     int     `json:"relatieId,omitempty"`
	Factuurnummer string  `json:"factuurnummer,omitempty"`
}

// errFactuurbetalingIncomplete is the per-item error for a soort 3/4 item
// without the relation or invoice number e-boekhouden needs to find the open
// invoice. User-facing Dutch.
var errFactuurbetalingIncomplete = errors.New("Relatie en factuurnummer zijn verplicht voor een factuurbetaling")

// buildFactuurbetalingRegel is the single mutatieRegel of a factuurbetaling:
// the gross amount, no BTW, and the relation plus invoice number that
// identify the open invoice. No tegenRekening and no inclusief/exclusief
// pair: e-boekhouden silently refuses the payment when they are present.
func buildFactuurbetalingRegel(bedrag float64, relatieID int, factuur string) map[string]any {
	return map[string]any{
		"index":     0,
		"bedrag":    abs(bedrag),
		"btw":       0,
		"btwCode":   "GEEN",
		"relatieId": relatieID,
		"factuur":   factuur,
	}
}

// buildBatchMutatiePayload builds the POST /v1/api/mutatie body for one batch
// item. mutDatum is the booking date (the bank line's date when known) and
// tegenRekeningID the resolved internal id of item.Grootboekcode.
//
// Soort 3 and 4 produce the factuurbetaling shape (see
// buildFactuurbetalingRegel) and fail with errFactuurbetalingIncomplete when
// the relation or invoice number is missing. Every other soort mirrors what
// the e-boekhouden web UI sends for a Geld uitgegeven / Geld ontvangen
// mutation: inEx on the mutation, and the gross amount split by the BTW code
// on the line so the booking lands in the BTW-aangifte.
func buildBatchMutatiePayload(item inboxBatchItem, mutDatum string, tegenRekeningID int) (map[string]any, error) {
	if isFactuurbetaling(item.Soort) {
		if item.RelatieId == 0 || strings.TrimSpace(item.Factuurnummer) == "" {
			return nil, errFactuurbetalingIncomplete
		}
		return map[string]any{
			"mutatie": map[string]any{
				"rekening":     item.GrootboekId,
				"datum":        mutDatum,
				"soort":        item.Soort,
				"omschrijving": item.Omschrijving,
			},
			"mutatieRegels": []map[string]any{buildFactuurbetalingRegel(item.Bedrag, item.RelatieId, item.Factuurnummer)},
			"importId":      item.ID,
		}, nil
	}

	// e-Boekhouden enforces a direction match between BTW code and soort:
	// income soorten (5 GeldOntvangen) require a *_VERK_* code, expense
	// soorten (6 GeldUitgegeven) require *_INK_*. Claude regularly picks the
	// wrong direction (especially for refunds of past purchases — it sees the
	// original purchase mental model and reaches for INK), so we normalize
	// the code to match the direction here.
	btwCode := normalizeBTWForSoort(item.BTWCode, item.Soort)

	// Same direction-aware logic for inEx: bank shows the gross amount, so we
	// always use IN for incoming and EX for outgoing.
	inEx := "EX"
	if item.Soort == 5 {
		inEx = "IN"
	}

	// The bank line bedrag is always gross (inclusive of BTW). Split it
	// according to the BTW code so e-boekhouden can include the booking in
	// the BTW-aangifte.
	rate := btwRateFromCode(btwCode)
	bedragIncl := item.Bedrag
	bedragExcl := item.Bedrag
	btwBedrag := 0.0
	if rate > 0 {
		btwBedrag = roundCents(bedragIncl * rate / (100 + rate))
		bedragExcl = roundCents(bedragIncl - btwBedrag)
	}

	// The inEx field on the mutation is REQUIRED ("EX" or "IN"); for
	// money-out/money-in soorten the bank shows the gross amount, so we pair
	// inEx with explicit bedragInclusief/bedragExclusief on the regel.
	regel := map[string]any{
		"index":           0,
		"bedrag":          bedragExcl,
		"btw":             btwBedrag,
		"btwCode":         btwCode,
		"tegenRekening":   tegenRekeningID,
		"bedragInclusief": bedragIncl,
		"bedragExclusief": bedragExcl,
	}
	// Optional fields — only include when actually set so the e-boekhouden
	// validator doesn't choke on empty strings or zero IDs.
	if item.RelatieId != 0 {
		regel["relatieId"] = item.RelatieId
	}
	if item.Factuurnummer != "" {
		regel["factuur"] = item.Factuurnummer
	}

	return map[string]any{
		"mutatie": map[string]any{
			"rekening":     item.GrootboekId,
			"datum":        mutDatum,
			"soort":        item.Soort,
			"inEx":         inEx,
			"omschrijving": item.Omschrijving,
		},
		"mutatieRegels": []map[string]any{regel},
		"importId":      item.ID,
	}, nil
}

// bankLine is the part of an unprocessed bank line the overlays need. It is
// built from import-grid rows on a cache miss and from the cached
// classification rows on a cache hit; both carry exactly these fields.
type bankLine struct {
	ID           int
	Datum        string
	Bedrag       float64
	Omschrijving string
}

// bankLinesFromRows reads import-grid rows as produced by ParseImportGrid.
func bankLinesFromRows(rows []map[string]any) []bankLine {
	lines := make([]bankLine, 0, len(rows))
	for _, row := range rows {
		id, _ := toInt(row["id"])
		bedrag, _ := toFloat(row["mutBedrag"])
		lines = append(lines, bankLine{
			ID:           id,
			Datum:        toString(row["mutDatum"]),
			Bedrag:       bedrag,
			Omschrijving: toString(row["mutOmschrijving"]),
		})
	}
	return lines
}

// bankLinesFromClassified reads the lines back out of cached classification
// rows, which were built one-to-one from the grid rows.
func bankLinesFromClassified(rows []inboxClassifiedRow) []bankLine {
	lines := make([]bankLine, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, bankLine{ID: row.ID, Datum: row.Datum, Bedrag: row.Bedrag, Omschrijving: row.Omschrijving})
	}
	return lines
}

// gridRowsFromBankLines renders lines in the import-grid row shape that
// rankBankLineMatches reads (id, mutDatum, mutBedrag, mutOmschrijving).
func gridRowsFromBankLines(lines []bankLine) []map[string]any {
	rows := make([]map[string]any, 0, len(lines))
	for _, l := range lines {
		rows = append(rows, map[string]any{
			"id":              float64(l.ID),
			"mutDatum":        l.Datum,
			"mutBedrag":       l.Bedrag,
			"mutOmschrijving": l.Omschrijving,
		})
	}
	return rows
}

// openPosts holds the outstanding invoices per side of the open-post grid.
// The two lists stay separate: outgoing bank lines pay purchase invoices
// (Crediteur), incoming lines settle sales invoices (Debiteur).
type openPosts struct {
	Crediteur []eboekhouden.OpenPost
	Debiteur  []eboekhouden.OpenPost
}

// openPostMatch is the open post a bank line pays. NameHit records whether a
// token of the post's company name occurs in the line's description; it sets
// the confidence of the suggestion.
type openPostMatch struct {
	Post    eboekhouden.OpenPost
	NameHit bool
}

// openPostAmountToleranceCents is the difference in whole cents between the
// bank amount and the outstanding amount of an open post from which the two
// no longer match: one cent off still matches, two cents does not. Amounts
// are compared in cents because both sides carry two decimals and a float
// subtraction of e.g. 30.02 and 30.00 lands just under 0.02.
const openPostAmountToleranceCents = 2

// amountMatchesOpenPost reports whether a bank amount (absolute) settles an
// open post's outstanding amount within the tolerance.
func amountMatchesOpenPost(amount, teOntvangen float64) bool {
	return math.Round(abs(teOntvangen-amount)*100) < openPostAmountToleranceCents
}

// matchOpenPosts pairs bank lines with the open posts they pay, keyed by bank
// line id. A line matches when exactly one post of its side has a positive
// outstanding amount within a cent of the line amount (see
// amountMatchesOpenPost); among
// several amount hits the one whose company name occurs as a whole word in
// the description wins, and when that still leaves none or several the line
// gets no match. Each post is suggested for at most one line: of several
// lines claiming one post the name hit wins, then the line dated closest on
// or after the post's date, then the lowest line id. The result does not
// depend on input order.
func matchOpenPosts(lines []bankLine, posts openPosts) map[int]openPostMatch {
	out := make(map[int]openPostMatch)
	matchOpenPostSide(out, lines, posts.Crediteur, false)
	matchOpenPostSide(out, lines, posts.Debiteur, true)
	return out
}

// matchOpenPostSide runs matchOpenPosts for one side: incoming lines against
// Debiteur posts, outgoing lines against Crediteur posts.
func matchOpenPostSide(out map[int]openPostMatch, lines []bankLine, posts []eboekhouden.OpenPost, incoming bool) {
	type claim struct {
		line    bankLine
		nameHit bool
	}
	// Step 1: each line picks its unique post, if any.
	claims := make(map[int][]claim) // post index → lines that pay it
	for _, line := range lines {
		if line.Bedrag == 0 || (line.Bedrag > 0) != incoming {
			continue
		}
		idx, nameHit, ok := uniqueOpenPost(line, posts)
		if !ok {
			continue
		}
		claims[idx] = append(claims[idx], claim{line: line, nameHit: nameHit})
	}
	// Step 2: each post keeps its best line. Lines claim at most one post, so
	// the map order cannot change which line ids end up in out.
	for idx, cs := range claims {
		post := posts[idx]
		postDate, hasDate := parseISODate(post.MutDatum)
		slices.SortFunc(cs, func(a, b claim) int {
			if a.nameHit != b.nameHit {
				if a.nameHit {
					return -1
				}
				return 1
			}
			if hasDate {
				ab, ad := openPostDateRank(postDate, a.line.Datum)
				bb, bd := openPostDateRank(postDate, b.line.Datum)
				if c := cmp.Compare(ab, bb); c != 0 {
					return c
				}
				if c := cmp.Compare(ad, bd); c != 0 {
					return c
				}
			}
			return cmp.Compare(a.line.ID, b.line.ID)
		})
		out[cs[0].line.ID] = openPostMatch{Post: post, NameHit: cs[0].nameHit}
	}
}

// uniqueOpenPost finds the one post line pays, as described at
// matchOpenPosts, and returns its index in posts.
func uniqueOpenPost(line bankLine, posts []eboekhouden.OpenPost) (idx int, nameHit bool, ok bool) {
	amount := abs(line.Bedrag)
	var amountHits []int
	for i, p := range posts {
		if p.TeOntvangen <= 0 || !amountMatchesOpenPost(amount, p.TeOntvangen) {
			continue
		}
		amountHits = append(amountHits, i)
	}
	switch len(amountHits) {
	case 0:
		return 0, false, false
	case 1:
		i := amountHits[0]
		return i, mentionsCompany(line.Omschrijving, posts[i].Bedrijf), true
	}
	var nameHits []int
	for _, i := range amountHits {
		if mentionsCompany(line.Omschrijving, posts[i].Bedrijf) {
			nameHits = append(nameHits, i)
		}
	}
	if len(nameHits) != 1 {
		return 0, false, false
	}
	return nameHits[0], true, true
}

// mentionsCompany reports whether a distinctive token of the company name
// (legal forms and short words dropped, see supplierNameTokens) occurs as a
// whole word in the bank description.
func mentionsCompany(omschrijving, bedrijf string) bool {
	return descriptionMentions(omschrijving, supplierNameTokens(bedrijf))
}

// openPostDateRank orders the lines that claim one post by date: bucket 0 for
// lines on or after the post's date, 1 for lines before it, 2 for unreadable
// dates; distance is the number of days between the two. Lower is better.
func openPostDateRank(postDate time.Time, lineDatum string) (bucket, distance int) {
	d, ok := parseISODate(lineDatum)
	if !ok {
		return 2, 0
	}
	days := int(math.Round(d.Sub(postDate).Hours() / 24))
	if days >= 0 {
		return 0, days
	}
	return 1, -days
}

// openPostIndicator is the row indicator for an open-post match:
// "Acme · INV-0042 · geboekt 12 okt". Empty parts are left out.
func openPostIndicator(p eboekhouden.OpenPost) string {
	bedrijf := strings.TrimSpace(p.Bedrijf)
	if bedrijf == "" {
		bedrijf = "Onbekende relatie"
	}
	parts := []string{bedrijf}
	if f := strings.TrimSpace(p.MutFactuur); f != "" {
		parts = append(parts, f)
	}
	if d, ok := parseISODate(p.MutDatum); ok {
		parts = append(parts, "geboekt "+formatDutchDate(d, false))
	}
	return strings.Join(parts, titleSeparator)
}

// inboxDocumentMatch names the analysed mail-folder document whose payment a
// bank line is.
type inboxDocumentMatch struct {
	FileID int    `json:"fileId"`
	Title  string `json:"title"`
}

// applyInboxOverlays returns base with the fresh open-post and document
// matches applied. An open post wins over everything: the invoice is already
// booked and the line just settles it. Otherwise a document match turns the
// line into an invoice line. base itself is not modified, so a cached base
// can be overlaid again on the next request.
func applyInboxOverlays(base []inboxClassifiedRow, posts map[int]openPostMatch, docs map[int]inboxDocumentMatch) []inboxClassifiedRow {
	out := make([]inboxClassifiedRow, len(base))
	for i, row := range base {
		if m, ok := posts[row.ID]; ok {
			row = withOpenPostMatch(row, m)
		} else if d, ok := docs[row.ID]; ok {
			row = withDocumentMatch(row, d)
		}
		out[i] = row
	}
	return out
}

// withOpenPostMatch turns a row into the factuurbetaling suggestion for m.
// The bank line's own description is used as booking description, as the
// e-boekhouden web UI does; the learned flag is dropped because the
// suggestion no longer comes from the user's memory.
func withOpenPostMatch(row inboxClassifiedRow, m openPostMatch) inboxClassifiedRow {
	row.Category = "auto"
	row.NeedsInvoice = false
	row.Confidence = 0.9
	if m.NameHit {
		row.Confidence = 1.0
	}
	row.Soort = soortNameFactuurbetalingOntvangen
	if row.Bedrag < 0 {
		row.Soort = soortNameFactuurbetalingVerstuurd
	}
	row.Grootboekcode = ""
	row.BTWCode = "GEEN"
	row.AIOmschrijving = row.Omschrijving
	row.Indicator = openPostIndicator(m.Post)
	row.Learned = false
	row.RelatieID = m.Post.RelatieID
	row.RelatieNaam = m.Post.Bedrijf
	row.Factuurnummer = m.Post.MutFactuur
	row.OpenPostMatch = true
	row.DocumentMatch = nil
	return row
}

// withDocumentMatch marks a row as the payment of a mail-folder document. The
// classification is kept as prefill for the review dialog.
func withDocumentMatch(row inboxClassifiedRow, d inboxDocumentMatch) inboxClassifiedRow {
	row.Category = "invoice"
	row.NeedsInvoice = true
	row.DocumentMatch = &d
	return row
}

// summarizeInbox counts the rows per category for the response summary.
func summarizeInbox(rows []inboxClassifiedRow) map[string]int {
	return map[string]int{
		"auto":    countCategory(rows, "auto"),
		"review":  countCategory(rows, "review"),
		"invoice": countCategory(rows, "invoice"),
		"manual":  countCategory(rows, "manual"),
	}
}

// documentMatchesByBankLine inverts a document list: for every document that
// is ready to book against its assigned bank line (status matched, so
// analysed, not linked, not ignored, not a duplicate), the line id maps to
// the document. The assignment is unique per line, so there are no clashes.
func documentMatchesByBankLine(docs []mailDocument) map[int]inboxDocumentMatch {
	out := make(map[int]inboxDocumentMatch)
	for _, d := range docs {
		if d.Status == docStatusMatched && d.matchedLineID > 0 {
			out[d.matchedLineID] = inboxDocumentMatch{FileID: d.ID, Title: d.Title}
		}
	}
	return out
}

// fetchOpenPosts loads both sides of the open-post grid for the overlay.
// Entirely best-effort, an expired session included: the bank-line inbox
// worked without these calls before, and e-boekhouden may answer a missing
// right or plan module the same way it answers an expired session, which
// must not turn into a reconnect loop on an inbox that works. A failure
// leaves that side empty (logged by error class); a real expiry surfaces on
// the import-grid fetch or the next booking.
func fetchOpenPosts(client *eboekhouden.Client) openPosts {
	var out openPosts
	sides := []struct {
		soort string
		dst   *[]eboekhouden.OpenPost
	}{
		{eboekhouden.OpenPostCrediteur, &out.Crediteur},
		{eboekhouden.OpenPostDebiteur, &out.Debiteur},
	}
	for _, side := range sides {
		raw, err := client.GetOpenPostGrid(side.soort)
		if err != nil {
			log.Printf("Inbox: open post grid %s fetch failed, no open-post overlay: %s", side.soort, errClass(err))
			if errors.Is(err, eboekhouden.ErrSessionExpired) {
				return openPosts{}
			}
			continue
		}
		posts, err := eboekhouden.ParseOpenPostGrid(raw)
		if err != nil {
			log.Printf("Inbox: open post grid %s parse failed: %s", side.soort, errClass(err))
			continue
		}
		*side.dst = posts
	}
	return out
}

// mailDocumentMatches annotates bank lines with the mail-folder documents
// they pay, using the same list builder and global assignment as GET
// /documents so both views agree. Entirely best-effort, for the same reason
// as fetchOpenPosts: no mail folder, an expired session, any e-boekhouden or
// Redis error all yield no annotations and never fail the inbox.
func mailDocumentMatches(ctx context.Context, rdb *redis.Client, client *eboekhouden.Client, userID string, lines []bankLine) map[int]inboxDocumentMatch {
	_, files, err := mailFolderFiles(client)
	if err != nil {
		if !errors.Is(err, errMailFolderNotConfigured) {
			log.Printf("Inbox: mail folder listing failed, no document annotations: %s", errClass(err))
		}
		return nil
	}
	if len(files) == 0 || len(lines) == 0 {
		return nil
	}
	docs, _, err := buildDocumentList(ctx, rdb, client, userID, files, staticGridRows(gridRowsFromBankLines(lines)))
	if err != nil {
		log.Printf("Inbox: document list failed, no document annotations: %s", errClass(err))
		return nil
	}
	return documentMatchesByBankLine(docs)
}
