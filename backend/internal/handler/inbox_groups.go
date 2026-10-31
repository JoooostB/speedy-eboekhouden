package handler

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math"
	"math/bits"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
)

// Payment groups: several incoming bank lines that together pay one sales
// invoice. A customer under a G-rekening arrangement (WKA) pays an invoice
// in parts spread over the bank account and the G-rekening, typically
// "exclusief minus 10%", "10% of exclusief" and "the BTW". e-boekhouden
// accepts a factuurbetaling (soort 3) per line with the same relatieId and
// factuur and only that line's amount, so booking a group is N ordinary
// batch items. What this file adds is recognising the group, which is pure
// and never cached (pitfall 13), and refusing a group that would pay more
// than is outstanding (factuurbetalingRefusals, used by BatchProcess).

// Group states and the evidence that put the lines together.
const (
	paymentGroupStateComplete  = "complete"  // the lines sum to the outstanding amount
	paymentGroupStatePartial   = "partial"   // the lines pay part of it; the rest stays open
	paymentGroupStateAmbiguous = "ambiguous" // the lines fit several posts, or several subsets fit

	paymentGroupEvidenceInvoice = "factuurnummer" // every line names the invoice
	paymentGroupEvidenceName    = "naam"          // at least one line only names the customer
)

// Search limits. Candidates are lines dated from the invoice date up to
// paymentGroupDateWindowDays after it; at most paymentGroupMaxCandidates
// per post take part in the exhaustive subset search (2^12 subsets), of
// which the sets of paymentGroupMinLines to paymentGroupMaxLines lines
// count. The invoice model (for the WKA amounts) is fetched for at most
// paymentGroupMaxModels posts per request.
const (
	paymentGroupDateWindowDays = 120
	paymentGroupMaxCandidates  = 12
	paymentGroupMinLines       = 2
	paymentGroupMaxLines       = 6
	paymentGroupMaxModels      = 10
)

// Invoice-number evidence: the full number cleaned to [A-Z0-9], or, because
// a bank truncates a description ("VB00420" arrives as "VB0042"), a prefix
// of at least invoiceRefMinPrefix characters that misses at most
// invoiceRefMaxTruncation characters, at the end of a description token.
const (
	invoiceRefMinPrefix     = 4
	invoiceRefMaxTruncation = 2
)

// isRefRune reports whether r counts in an invoice reference: an ASCII
// letter or digit. Everything else, accented letters included, is a
// separator, so byte and character positions agree in strippedRef.
func isRefRune(r rune) bool {
	return r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

// paymentGroupPost is the open post a group pays, as the frontend shows it.
type paymentGroupPost struct {
	RelatieID     int     `json:"relatieId"`
	RelatieNaam   string  `json:"relatieNaam"`
	Factuurnummer string  `json:"factuurnummer"`
	Factuurdatum  string  `json:"factuurdatum"`
	SBedrag       float64 `json:"sBedrag"`
	SBetaald      float64 `json:"sBetaald"`
	TeOntvangen   float64 `json:"teOntvangen"`
}

// paymentGroupCandidate is one of the posts an ambiguous group may pay,
// with the lines of the group that fit it.
type paymentGroupCandidate struct {
	paymentGroupPost
	LineIDs []int `json:"lineIds"`
}

// paymentGroup is one group in the classify response. For a complete or
// partial group the post fields name the invoice and Candidates holds that
// same post; for an ambiguous group the post fields are empty and the user
// picks among Candidates. Category is "auto" only for a complete group whose
// every line names the invoice; everything else is "review".
type paymentGroup struct {
	ID       string `json:"id"`
	State    string `json:"state"`
	Category string `json:"category"`
	paymentGroupPost
	LineIDs    []int                   `json:"lineIds"`
	Total      float64                 `json:"total"`
	Candidates []paymentGroupCandidate `json:"candidates"`
	Evidence   string                  `json:"evidence"`
}

// groupCandidate is a bank line that may belong to a post's group, with the
// evidence that made it a candidate.
type groupCandidate struct {
	line       bankLine
	invoiceHit bool
	nameHit    bool
}

// groupProposal is one way a set of lines pays one post, before conflicts
// between proposals are resolved.
type groupProposal struct {
	post    eboekhouden.OpenPost
	lineIDs []int // sorted
	state   string
	// invoiceOnly says every line names the invoice number.
	invoiceOnly bool
}

// postFromOpenPost copies the fields the frontend shows.
func postFromOpenPost(p eboekhouden.OpenPost) paymentGroupPost {
	return paymentGroupPost{
		RelatieID:     p.RelatieID,
		RelatieNaam:   p.Bedrijf,
		Factuurnummer: p.MutFactuur,
		Factuurdatum:  p.MutDatum,
		SBedrag:       p.SBedrag,
		SBetaald:      p.SBetaald,
		TeOntvangen:   p.TeOntvangen,
	}
}

// cents rounds an amount to whole cents so sums can be compared exactly.
func cents(v float64) int64 {
	return int64(math.Round(v * 100))
}

// centsMatch reports whether two amounts in cents are within the open-post
// tolerance (one cent off still matches).
func centsMatch(a, b int64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < openPostAmountToleranceCents
}

// normalizeRef reduces an invoice number or a description to upper-case
// ASCII letters and digits: "INV-0042" and "inv 0042" both become "INV0042".
func normalizeRef(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if isRefRune(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// refTokens are the runs of letters and digits of a description, upper-cased.
func refTokens(s string) []string {
	return strings.FieldsFunc(strings.ToUpper(s), func(r rune) bool { return !isRefRune(r) })
}

// isDigits reports whether s is non-empty and all digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// strippedRef is a description with separators removed, plus for every
// character whether a token ends there in the original (the next character
// is a separator or the end). The truncation rule needs that boundary.
func strippedRef(s string) (stripped string, tokenEnd []bool) {
	runes := []rune(strings.ToUpper(s))
	var b strings.Builder
	for i, r := range runes {
		if !isRefRune(r) {
			continue
		}
		b.WriteRune(r)
		next := i + 1
		tokenEnd = append(tokenEnd, next >= len(runes) || !isRefRune(runes[next]))
	}
	return b.String(), tokenEnd
}

// mentionsInvoiceNumber reports whether a bank description names an invoice
// number. Both sides are reduced to [A-Z0-9]. The full number must occur in
// the description, or, for a purely numeric number (which would otherwise
// be found inside an IBAN or a date), as a whole token. A truncated number
// also counts: a prefix of at least invoiceRefMinPrefix characters missing
// at most invoiceRefMaxTruncation characters, ending where a token ends
// ("VF-2026-001" for VF-2026-0017), so "INV-00" never claims INV-0043 for
// INV-0042. Numbers shorter than invoiceRefMinPrefix never match.
func mentionsInvoiceNumber(omschrijving, factuur string) bool {
	ref := normalizeRef(factuur)
	if len(ref) < invoiceRefMinPrefix {
		return false
	}
	minPrefix := max(invoiceRefMinPrefix, len(ref)-invoiceRefMaxTruncation)
	if isDigits(ref) {
		for _, tok := range refTokens(omschrijving) {
			if len(tok) >= minPrefix && len(tok) <= len(ref) && strings.HasPrefix(ref, tok) {
				return true
			}
		}
		return false
	}
	stripped, tokenEnd := strippedRef(omschrijving)
	if strings.Contains(stripped, ref) {
		return true
	}
	for n := len(ref) - 1; n >= minPrefix; n-- {
		prefix := ref[:n]
		for at := strings.Index(stripped, prefix); at >= 0; {
			if tokenEnd[at+n-1] {
				return true
			}
			next := strings.Index(stripped[at+1:], prefix)
			if next < 0 {
				break
			}
			at += 1 + next
		}
	}
	return false
}

// withinGroupDateWindow reports whether a line is dated on or after the
// post's date and at most paymentGroupDateWindowDays later. A date that
// cannot be read does not exclude the line.
func withinGroupDateWindow(post eboekhouden.OpenPost, line bankLine) bool {
	postDate, ok := parseISODate(post.MutDatum)
	if !ok {
		return true
	}
	lineDate, ok := parseISODate(line.Datum)
	if !ok {
		return true
	}
	days := int(math.Round(lineDate.Sub(postDate).Hours() / 24))
	return days >= 0 && days <= paymentGroupDateWindowDays
}

// nameHitClaims returns the lines and posts (by mutNr) that a single match
// with a name hit already settled. Those are final: a group never takes
// them. A single match on the amount alone is weaker than a group and stays
// available.
func nameHitClaims(singles map[int]openPostMatch) (lines map[int]bool, posts map[int]bool) {
	lines = make(map[int]bool)
	posts = make(map[int]bool)
	for id, m := range singles {
		if m.NameHit {
			lines[id] = true
			posts[m.Post.MutNr] = true
		}
	}
	return lines, posts
}

// postCandidates lists the incoming lines that may pay post: not claimed by
// a name-hit single match, inside the date window, and naming the invoice
// number or the customer. Never on the amount alone. Sorted by evidence
// (invoice number first), date, id and capped at paymentGroupMaxCandidates,
// so the result does not depend on input order.
func postCandidates(post eboekhouden.OpenPost, lines []bankLine, claimed map[int]bool) []groupCandidate {
	var out []groupCandidate
	for _, line := range lines {
		if line.Bedrag <= 0 || claimed[line.ID] || !withinGroupDateWindow(post, line) {
			continue
		}
		c := groupCandidate{
			line:       line,
			invoiceHit: mentionsInvoiceNumber(line.Omschrijving, post.MutFactuur),
			nameHit:    mentionsCompany(line.Omschrijving, post.Bedrijf),
		}
		if !c.invoiceHit && !c.nameHit {
			continue
		}
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b groupCandidate) int {
		if a.invoiceHit != b.invoiceHit {
			if a.invoiceHit {
				return -1
			}
			return 1
		}
		if c := cmp.Compare(a.line.Datum, b.line.Datum); c != 0 {
			return c
		}
		return cmp.Compare(a.line.ID, b.line.ID)
	})
	if len(out) > paymentGroupMaxCandidates {
		out = out[:paymentGroupMaxCandidates]
	}
	return out
}

// completeSubsets finds every set of paymentGroupMinLines to
// paymentGroupMaxLines candidates whose amounts sum to teOntvangen within
// the tolerance. Each result is a sorted list of line ids, and the results
// are sorted too.
func completeSubsets(cands []groupCandidate, teOntvangen float64) [][]int {
	n := len(cands)
	amounts := make([]int64, n)
	for i, c := range cands {
		amounts[i] = cents(c.line.Bedrag)
	}
	target := cents(teOntvangen)
	var found [][]int
	for mask := uint(1); mask < 1<<n; mask++ {
		size := bits.OnesCount(mask)
		if size < paymentGroupMinLines || size > paymentGroupMaxLines {
			continue
		}
		var sum int64
		for i := 0; i < n; i++ {
			if mask&(1<<i) != 0 {
				sum += amounts[i]
			}
		}
		if !centsMatch(sum, target) {
			continue
		}
		ids := make([]int, 0, size)
		for i := 0; i < n; i++ {
			if mask&(1<<i) != 0 {
				ids = append(ids, cands[i].line.ID)
			}
		}
		slices.Sort(ids)
		found = append(found, ids)
	}
	slices.SortFunc(found, slices.Compare)
	return found
}

// wkaAmounts are the parts a G-rekening customer pays an invoice in: the
// amount excluding BTW minus 10%, the 10% itself, the BTW, and the 10% plus
// the BTW (what lands on the G-rekening in one go). Empty without totals.
func wkaAmounts(t eboekhouden.MutatieTotals) []int64 {
	excl := cents(t.Exclusief)
	if excl <= 0 {
		return nil
	}
	tenPercent := int64(math.Round(float64(excl) / 10))
	btw := cents(t.BTW)
	return []int64{excl - tenPercent, tenPercent, btw, tenPercent + btw}
}

// matchesAnyAmount reports whether amount matches one of the targets within
// the tolerance.
func matchesAnyAmount(amount int64, targets []int64) bool {
	for _, t := range targets {
		if t > 0 && centsMatch(amount, t) {
			return true
		}
	}
	return false
}

// partialLines picks the candidates that pay part of a post with strong
// evidence: a line naming the invoice number, or a line naming the customer
// whose amount (alone, or the name-only lines together) is one of the WKA
// amounts of the invoice. Without the invoice model only the invoice number
// counts. The result is nil when nothing qualifies, when the lines together
// reach the outstanding amount (that is a complete group or a single match)
// or exceed it (an overpayment is never proposed).
func partialLines(cands []groupCandidate, teOntvangen float64, totals eboekhouden.MutatieTotals, hasModel bool) (ids []int, invoiceOnly bool) {
	var wka []int64
	if hasModel {
		wka = wkaAmounts(totals)
	}
	var strong, nameOnly []groupCandidate
	for _, c := range cands {
		switch {
		case c.invoiceHit:
			strong = append(strong, c)
		case matchesAnyAmount(cents(c.line.Bedrag), wka):
			strong = append(strong, c)
		default:
			nameOnly = append(nameOnly, c)
		}
	}
	if len(nameOnly) > 0 {
		var sum int64
		for _, c := range nameOnly {
			sum += cents(c.line.Bedrag)
		}
		if matchesAnyAmount(sum, wka) {
			strong = append(strong, nameOnly...)
		}
	}
	if len(strong) == 0 {
		return nil, false
	}
	var sum int64
	invoiceOnly = true
	for _, c := range strong {
		sum += cents(c.line.Bedrag)
		ids = append(ids, c.line.ID)
		invoiceOnly = invoiceOnly && c.invoiceHit
	}
	if sum > cents(teOntvangen)-openPostAmountToleranceCents {
		return nil, false
	}
	slices.Sort(ids)
	return ids, invoiceOnly
}

// invoiceOnlyLines reports whether every id names the invoice.
func invoiceOnlyLines(cands []groupCandidate, ids []int) bool {
	for _, c := range cands {
		if slices.Contains(ids, c.line.ID) && !c.invoiceHit {
			return false
		}
	}
	return true
}

// groupProposals runs the search per post: the complete subsets when there
// are any, else the partial set. Posts that are not open or that a name-hit
// single match already settles are skipped. needModel receives the posts
// (by index) with a name-only candidate and no complete subset: those are
// the ones whose invoice model would help.
func groupProposals(lines []bankLine, posts []eboekhouden.OpenPost, singles map[int]openPostMatch, models map[int]eboekhouden.MutatieTotals) (proposals []groupProposal, needModel []int) {
	claimedLines, claimedPosts := nameHitClaims(singles)
	for i, post := range posts {
		if post.TeOntvangen <= 0 || claimedPosts[post.MutNr] {
			continue
		}
		cands := postCandidates(post, lines, claimedLines)
		if len(cands) == 0 {
			continue
		}
		if subsets := completeSubsets(cands, post.TeOntvangen); len(subsets) > 0 {
			for _, ids := range subsets {
				proposals = append(proposals, groupProposal{post: post, lineIDs: ids, state: paymentGroupStateComplete, invoiceOnly: invoiceOnlyLines(cands, ids)})
			}
			continue
		}
		nameOnly := false
		for _, c := range cands {
			if !c.invoiceHit {
				nameOnly = true
			}
		}
		totals, hasModel := models[post.MutNr]
		if nameOnly && !hasModel {
			needModel = append(needModel, i)
		}
		if ids, invoiceOnly := partialLines(cands, post.TeOntvangen, totals, hasModel); len(ids) > 0 {
			proposals = append(proposals, groupProposal{post: post, lineIDs: ids, state: paymentGroupStatePartial, invoiceOnly: invoiceOnly})
		}
	}
	return proposals, needModel
}

// paymentGroupModelTargets lists the mutNrs of the posts whose invoice model
// is worth fetching before matchOpenPostGroups: posts with a candidate that
// only names the customer and no complete group yet. Newest post first, at
// most paymentGroupMaxModels.
func paymentGroupModelTargets(lines []bankLine, posts []eboekhouden.OpenPost, singles map[int]openPostMatch) []int {
	_, need := groupProposals(lines, posts, singles, nil)
	slices.SortFunc(need, func(a, b int) int {
		if c := cmp.Compare(posts[b].MutDatum, posts[a].MutDatum); c != 0 {
			return c
		}
		return cmp.Compare(posts[b].MutNr, posts[a].MutNr)
	})
	var out []int
	for _, i := range need {
		if posts[i].MutNr <= 0 {
			continue
		}
		out = append(out, posts[i].MutNr)
		if len(out) == paymentGroupMaxModels {
			break
		}
	}
	return out
}

// paymentGroupID is a stable id for a set of lines: the first 16 hex digits
// of the SHA-256 of the sorted ids. The same lines always get the same id,
// whatever the request.
func paymentGroupID(lineIDs []int) string {
	ids := slices.Clone(lineIDs)
	slices.Sort(ids)
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, ",")))
	return "pg-" + hex.EncodeToString(sum[:8])
}

// matchOpenPostGroups finds the groups of incoming lines that pay one
// Debiteur post each. singles are the single-line matches of matchOpenPosts:
// a name-hit single is final and its line and post are left alone, while an
// amount-only single is weaker than a group (the caller drops it for lines
// that end up in a group, see withoutGroupedLines). models holds the invoice
// totals per mutNr for the WKA rule of partial groups; it may be nil.
//
// Precedence: a single match with a name hit, then a complete group, then a
// partial group, then a single match on the amount alone. A line is in at
// most one group: proposals that share a line, or several subsets that fit
// one post, are merged into one ambiguous group with the posts as
// candidates, and nothing is picked for the user. The result is sorted by
// the lowest line id and does not depend on input order.
func matchOpenPostGroups(lines []bankLine, posts []eboekhouden.OpenPost, singles map[int]openPostMatch, models map[int]eboekhouden.MutatieTotals) []paymentGroup {
	proposals, _ := groupProposals(lines, posts, singles, models)
	if len(proposals) == 0 {
		return nil
	}

	// Union-find over proposals: two proposals are in one component when
	// they share a line.
	parent := make([]int, len(proposals))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	byLine := make(map[int]int)
	for i, p := range proposals {
		for _, id := range p.lineIDs {
			if j, ok := byLine[id]; ok {
				parent[find(i)] = find(j)
			} else {
				byLine[id] = i
			}
		}
	}
	components := make(map[int][]groupProposal)
	for i, p := range proposals {
		root := find(i)
		components[root] = append(components[root], p)
	}

	amounts := make(map[int]float64, len(lines))
	for _, l := range lines {
		amounts[l.ID] = l.Bedrag
	}
	var groups []paymentGroup
	for _, ps := range components {
		groups = append(groups, buildPaymentGroup(ps, amounts))
	}
	slices.SortFunc(groups, func(a, b paymentGroup) int {
		if c := slices.Compare(a.LineIDs, b.LineIDs); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return groups
}

// buildPaymentGroup turns one component of proposals into a group. A
// complete proposal outranks the partial ones it shares a line with (those
// are dropped), a lone proposal keeps its state, and anything else is
// ambiguous.
func buildPaymentGroup(ps []groupProposal, amounts map[int]float64) paymentGroup {
	if slices.ContainsFunc(ps, func(p groupProposal) bool { return p.state == paymentGroupStateComplete }) {
		ps = slices.DeleteFunc(slices.Clone(ps), func(p groupProposal) bool { return p.state != paymentGroupStateComplete })
	}
	slices.SortFunc(ps, func(a, b groupProposal) int {
		if c := cmp.Compare(a.post.MutDatum, b.post.MutDatum); c != 0 {
			return c
		}
		if c := cmp.Compare(a.post.MutNr, b.post.MutNr); c != 0 {
			return c
		}
		return slices.Compare(a.lineIDs, b.lineIDs)
	})
	var g paymentGroup
	if len(ps) == 1 {
		p := ps[0]
		g.State = p.state
		g.paymentGroupPost = postFromOpenPost(p.post)
		g.LineIDs = slices.Clone(p.lineIDs)
		g.Candidates = []paymentGroupCandidate{{paymentGroupPost: g.paymentGroupPost, LineIDs: slices.Clone(p.lineIDs)}}
		g.Evidence = paymentGroupEvidenceName
		if p.invoiceOnly {
			g.Evidence = paymentGroupEvidenceInvoice
		}
		g.Category = "review"
		if p.state == paymentGroupStateComplete && p.invoiceOnly {
			g.Category = "auto"
		}
	} else {
		g.State = paymentGroupStateAmbiguous
		g.Category = "review"
		g.Evidence = paymentGroupEvidenceName
		seen := make(map[int]bool)
		allInvoice := true
		for _, p := range ps {
			for _, id := range p.lineIDs {
				if !slices.Contains(g.LineIDs, id) {
					g.LineIDs = append(g.LineIDs, id)
				}
			}
			allInvoice = allInvoice && p.invoiceOnly
			if seen[p.post.MutNr] {
				continue // several subsets fit this post: the first (sorted) one is the candidate's prefill
			}
			seen[p.post.MutNr] = true
			g.Candidates = append(g.Candidates, paymentGroupCandidate{paymentGroupPost: postFromOpenPost(p.post), LineIDs: slices.Clone(p.lineIDs)})
		}
		if allInvoice {
			g.Evidence = paymentGroupEvidenceInvoice
		}
		slices.Sort(g.LineIDs)
	}
	for _, id := range g.LineIDs {
		g.Total += amounts[id]
	}
	g.Total = roundCents(g.Total)
	g.ID = paymentGroupID(g.LineIDs)
	return g
}

// withoutGroupedLines drops the single matches of lines that are in a group
// (those were amount-only matches; a name hit keeps the line out of groups)
// and the amount-only single matches on a post that a group now pays, so the
// inbox never suggests one post twice. singles itself is not modified.
func withoutGroupedLines(singles map[int]openPostMatch, groups []paymentGroup) map[int]openPostMatch {
	if len(groups) == 0 {
		return singles
	}
	grouped := make(map[int]bool)
	takenPosts := make(map[string]bool)
	for _, g := range groups {
		for _, id := range g.LineIDs {
			grouped[id] = true
		}
		if g.State != paymentGroupStateAmbiguous {
			takenPosts[strconv.Itoa(g.RelatieID)+"|"+strings.ToUpper(strings.TrimSpace(g.Factuurnummer))] = true
		}
	}
	out := make(map[int]openPostMatch, len(singles))
	for id, m := range singles {
		if grouped[id] {
			continue
		}
		if !m.NameHit && takenPosts[strconv.Itoa(m.Post.RelatieID)+"|"+strings.ToUpper(strings.TrimSpace(m.Post.MutFactuur))] {
			continue
		}
		out[id] = m
	}
	return out
}

// paymentGroupIndicator is the row indicator of a grouped line:
// "Acme · INV-0042 · betaling in delen" / "… · deelbetaling", or for an
// ambiguous group "Past bij 2 openstaande facturen van Acme".
func paymentGroupIndicator(g paymentGroup) string {
	if g.State == paymentGroupStateAmbiguous {
		n := len(g.Candidates)
		names := make(map[string]bool)
		for _, c := range g.Candidates {
			names[strings.TrimSpace(c.RelatieNaam)] = true
		}
		s := fmt.Sprintf("Past bij %d openstaande facturen", n)
		if len(names) == 1 && n > 0 && strings.TrimSpace(g.Candidates[0].RelatieNaam) != "" {
			s += " van " + strings.TrimSpace(g.Candidates[0].RelatieNaam)
		}
		return s
	}
	bedrijf := strings.TrimSpace(g.RelatieNaam)
	if bedrijf == "" {
		bedrijf = "Onbekende relatie"
	}
	parts := []string{bedrijf}
	if f := strings.TrimSpace(g.Factuurnummer); f != "" {
		parts = append(parts, f)
	}
	if g.State == paymentGroupStatePartial {
		parts = append(parts, "deelbetaling")
	} else {
		parts = append(parts, "betaling in delen")
	}
	return strings.Join(parts, titleSeparator)
}

// withPaymentGroup turns a row into the factuurbetaling suggestion of its
// group, the same shape as withOpenPostMatch. An ambiguous group leaves the
// relation and invoice empty: the user picks the post in the dialog.
func withPaymentGroup(row inboxClassifiedRow, g paymentGroup) inboxClassifiedRow {
	row.Category = g.Category
	row.NeedsInvoice = false
	row.Confidence = 0.9
	if g.Category == "auto" {
		row.Confidence = 1.0
	}
	row.Soort = soortNameFactuurbetalingOntvangen
	row.Grootboekcode = ""
	row.BTWCode = "GEEN"
	row.AIOmschrijving = row.Omschrijving
	row.Indicator = paymentGroupIndicator(g)
	row.Learned = false
	row.RelatieID = g.RelatieID
	row.RelatieNaam = g.RelatieNaam
	row.Factuurnummer = g.Factuurnummer
	row.OpenPostMatch = false
	row.DocumentMatch = nil
	row.PaymentGroupID = g.ID
	return row
}

// applyPaymentGroups marks the rows of every group. A group wins over the
// document overlay for the same reason an open post does: the invoice is
// already booked and the lines only settle it. rows is not modified.
func applyPaymentGroups(rows []inboxClassifiedRow, groups []paymentGroup) []inboxClassifiedRow {
	if len(groups) == 0 {
		return rows
	}
	byLine := make(map[int]paymentGroup)
	for _, g := range groups {
		for _, id := range g.LineIDs {
			byLine[id] = g
		}
	}
	out := make([]inboxClassifiedRow, len(rows))
	for i, row := range rows {
		if g, ok := byLine[row.ID]; ok {
			row = withPaymentGroup(row, g)
		}
		out[i] = row
	}
	return out
}

// groupedAutoLines lists the lines of complete groups whose every line names
// the invoice: like a name-hit single match these need no classification,
// so classifyFresh keeps them out of the Claude batch.
func groupedAutoLines(groups []paymentGroup) map[int]bool {
	out := make(map[int]bool)
	for _, g := range groups {
		if g.State == paymentGroupStateComplete && g.Category == "auto" {
			for _, id := range g.LineIDs {
				out[id] = true
			}
		}
	}
	return out
}

// fetchMutatieTotals reads the invoice totals of the given mutations. Best
// effort like fetchOpenPosts: a failure leaves that post without a model
// (the invoice-number rule still works), and an expired session stops the
// loop without failing the inbox.
func fetchMutatieTotals(client *eboekhouden.Client, mutNrs []int) map[int]eboekhouden.MutatieTotals {
	out := make(map[int]eboekhouden.MutatieTotals, len(mutNrs))
	for _, nr := range mutNrs {
		raw, err := client.GetMutatieModel(nr)
		if err != nil {
			log.Printf("Inbox: mutatie model %d fetch failed: %s", nr, errClass(err))
			if errors.Is(err, eboekhouden.ErrSessionExpired) {
				return out
			}
			continue
		}
		totals, err := eboekhouden.ParseMutatieTotals(raw)
		if err != nil {
			log.Printf("Inbox: mutatie model %d parse failed: %s", nr, errClass(err))
			continue
		}
		out[nr] = totals
	}
	return out
}

// openPostOverlays computes both open-post overlays for a classify response:
// the single matches and the payment groups, with the groups' precedence
// applied to the singles. One grid fetch per side and at most
// paymentGroupMaxModels model fetches; all best effort.
func openPostOverlays(client *eboekhouden.Client, lines []bankLine) (map[int]openPostMatch, []paymentGroup) {
	posts := fetchOpenPosts(client)
	singles := matchOpenPosts(lines, posts)
	models := fetchMutatieTotals(client, paymentGroupModelTargets(lines, posts.Debiteur, singles))
	groups := matchOpenPostGroups(lines, posts.Debiteur, singles, models)
	return withoutGroupedLines(singles, groups), groups
}

// Refusal messages of the BatchProcess guard. User-facing Dutch.
const (
	msgOpenPostsUnavailable = "De openstaande facturen konden niet worden opgehaald uit e-Boekhouden. Probeer het opnieuw."
	msgOpenPostChanged      = "De factuur is in e-Boekhouden veranderd: er staat nu %s open. Controleer de betalingen en probeer het opnieuw."
)

// factuurbetalingKey identifies the open post a soort 3/4 item pays.
func factuurbetalingKey(relatieID int, factuur string) string {
	return strconv.Itoa(relatieID) + "|" + strings.ToUpper(strings.TrimSpace(factuur))
}

// factuurbetalingRefusals checks the soort 3/4 items of a batch against the
// open-post grid before anything is booked: the items on one post (relation
// plus invoice number) are summed, and all of them are refused when the post
// is no longer open or the sum exceeds what is outstanding by more than a
// cent. The result maps item ids to the per-item error. posts is the fresh
// grid; unavailable names the sides whose grid could not be fetched, whose
// items are refused too rather than booked blind. Items without relation or
// invoice number are left to buildBatchMutatiePayload.
func factuurbetalingRefusals(items []inboxBatchItem, posts openPosts, unavailable map[string]bool) map[int]string {
	out := make(map[int]string)
	type bucket struct {
		soort int
		ids   []int
		sum   int64
	}
	buckets := make(map[string]*bucket)
	var order []string
	for _, item := range items {
		if !isFactuurbetaling(item.Soort) || item.RelatieId == 0 || strings.TrimSpace(item.Factuurnummer) == "" {
			continue
		}
		key := factuurbetalingKey(item.RelatieId, item.Factuurnummer)
		b, ok := buckets[key]
		if !ok {
			b = &bucket{soort: item.Soort}
			buckets[key] = b
			order = append(order, key)
		}
		b.ids = append(b.ids, item.ID)
		b.sum += cents(abs(item.Bedrag))
	}
	for _, key := range order {
		b := buckets[key]
		side, list := eboekhouden.OpenPostDebiteur, posts.Debiteur
		if b.soort == soortFactuurbetalingVerstuurd {
			side, list = eboekhouden.OpenPostCrediteur, posts.Crediteur
		}
		if unavailable[side] {
			for _, id := range b.ids {
				out[id] = msgOpenPostsUnavailable
			}
			continue
		}
		var open float64
		for _, p := range list {
			if factuurbetalingKey(p.RelatieID, p.MutFactuur) == key && p.TeOntvangen > 0 {
				open = p.TeOntvangen
				break
			}
		}
		if open <= 0 || b.sum > cents(open)+openPostAmountToleranceCents-1 {
			msg := fmt.Sprintf(msgOpenPostChanged, formatMoney("EUR", open))
			for _, id := range b.ids {
				out[id] = msg
			}
		}
	}
	return out
}

// factuurbetalingSides lists the open-post sides the soort 3/4 items of a
// batch need: Debiteur for received payments, Crediteur for sent ones.
func factuurbetalingSides(items []inboxBatchItem) []string {
	var sides []string
	for _, item := range items {
		if !isFactuurbetaling(item.Soort) || item.RelatieId == 0 || strings.TrimSpace(item.Factuurnummer) == "" {
			continue
		}
		side := eboekhouden.OpenPostDebiteur
		if item.Soort == soortFactuurbetalingVerstuurd {
			side = eboekhouden.OpenPostCrediteur
		}
		if !slices.Contains(sides, side) {
			sides = append(sides, side)
		}
	}
	slices.Sort(sides)
	return sides
}

// factuurbetalingGuard fetches the open-post grid for the sides a batch
// needs and returns the refusals. An expired session is returned as the
// error so BatchProcess answers 412 before booking anything; any other fetch
// failure refuses that side's items (msgOpenPostsUnavailable) and lets the
// rest of the batch proceed.
func factuurbetalingGuard(client *eboekhouden.Client, items []inboxBatchItem) (map[int]string, error) {
	sides := factuurbetalingSides(items)
	if len(sides) == 0 {
		return nil, nil
	}
	var posts openPosts
	unavailable := make(map[string]bool)
	for _, side := range sides {
		raw, err := client.GetOpenPostGrid(side)
		if err != nil {
			if errors.Is(err, eboekhouden.ErrSessionExpired) {
				return nil, err
			}
			log.Printf("Inbox: open post grid %s fetch failed before booking: %s", side, errClass(err))
			unavailable[side] = true
			continue
		}
		list, err := eboekhouden.ParseOpenPostGrid(raw)
		if err != nil {
			log.Printf("Inbox: open post grid %s parse failed before booking: %s", side, errClass(err))
			unavailable[side] = true
			continue
		}
		if side == eboekhouden.OpenPostDebiteur {
			posts.Debiteur = list
		} else {
			posts.Crediteur = list
		}
	}
	return factuurbetalingRefusals(items, posts, unavailable), nil
}
