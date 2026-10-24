package handler

import (
	"cmp"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/joooostb/speedy-eboekhouden/internal/claude"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
)

// Statuses of a mail-folder document, in priority order: the first rule that
// applies wins (see documentStatus).
const (
	docStatusLinked         = "linked"          // already linked to a mutation in e-boekhouden
	docStatusIgnored        = "ignored"         // hidden by the user in Speedy
	docStatusUnsupported    = "unsupported"     // extension the analyser cannot read (XML/UBL, …)
	docStatusJunk           = "junk"            // tiny image: logo, signature, tracking pixel
	docStatusDuplicate      = "duplicate"       // the same bytes were booked before
	docStatusNotInvoice     = "not_invoice"     // analysed as "overig"
	docStatusMatched        = "matched"         // analysed and assigned to a bank line
	docStatusReceiptWaiting = "receipt_waiting" // bonnetje without a bank line yet
	docStatusUnmatched      = "unmatched"       // analysed, no bank line
	docStatusNew            = "new"             // not analysed
)

// junkImageMaxSize is the size below which an image is treated as junk
// without spending an AI call: no readable receipt or invoice fits in it.
const junkImageMaxSize = 15 * 1024

// docStatusInput collects everything documentStatus looks at.
type docStatusInput struct {
	KoppelCount int
	Ignored     bool
	// MediaType is the type derived from the file extension; "" means the
	// extension is not one the analyser reads.
	MediaType string
	Size      int64
	// Analysis is the cached analysis, nil when the document was never analysed.
	Analysis *claude.InvoiceData
	// AlreadySubmitted is true when an invoice:submitted marker exists for
	// the analysed file's content hash.
	AlreadySubmitted bool
	// HasMatch is true when a bank line is assigned to the document.
	HasMatch bool
}

// documentStatus applies the status rules in priority order.
func documentStatus(in docStatusInput) string {
	docType := ""
	if in.Analysis != nil {
		docType = strings.ToLower(strings.TrimSpace(in.Analysis.DocumentType))
	}
	switch {
	case in.KoppelCount > 0:
		return docStatusLinked
	case in.Ignored:
		return docStatusIgnored
	case in.MediaType == "":
		return docStatusUnsupported
	case strings.HasPrefix(in.MediaType, "image/") && in.Size < junkImageMaxSize:
		return docStatusJunk
	case in.AlreadySubmitted:
		return docStatusDuplicate
	case in.Analysis == nil:
		return docStatusNew
	case docType == claude.DocumentTypeOverig:
		return docStatusNotInvoice
	case in.HasMatch:
		return docStatusMatched
	case docType == claude.DocumentTypeBonnetje:
		return docStatusReceiptWaiting
	default:
		return docStatusUnmatched
	}
}

// mediaTypeFromExtension maps a file name to the media type its extension
// implies: pdf → application/pdf, jpg/jpeg → image/jpeg, png → image/png.
// Anything else yields "" (unsupported). The bytes are never inspected here;
// the analyse and content endpoints sniff them when they download the file.
func mediaTypeFromExtension(name string) string {
	_, ext := splitFileName(name)
	switch strings.ToLower(ext) {
	case "pdf":
		return "application/pdf"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	}
	return ""
}

// splitFileName returns the base name and the extension (without the dot) of
// a file name. A name without a dot, or starting with its only dot, has no
// extension.
func splitFileName(name string) (base, ext string) {
	i := strings.LastIndex(name, ".")
	if i <= 0 {
		return name, ""
	}
	return name[:i], name[i+1:]
}

// titleSeparator joins the parts of a title or subtitle.
const titleSeparator = " · "

// documentTitle returns the title and subtitle of a document for the list.
//
// Before analysis the title is the file name without extension, with "_" and
// "-" turned into spaces, and the subtitle describes the file:
//
//	"invoice 8821"  /  "Ontvangen 12 okt 14:32 · PDF · 84 kB"
//
// After analysis the title names the supplier, invoice date and total in
// nl-NL formatting, and the subtitle the document type, number and file:
//
//	"Anthropic · 12 okt 2026 · € 1.234,56"  /  "Factuur INV-8821 · invoice_8821.pdf"
//	"Bonnetje Albert Heijn · 3 okt 2026 · € 12,50"  /  "Bonnetje · bon.jpg"
//	"Tesla · 1 okt 2026 · CHF 32,80"  /  "Factuur 4711 · tesla.pdf · ≈ € 34,44"
//
// A missing supplier reads "Onbekende leverancier". A document analysed as
// "overig" keeps the file-name title; its subtitle is Claude's reason.
func documentTitle(file eboekhouden.ArchiveFile, inv *claude.InvoiceData) (title, subtitle string) {
	if inv == nil {
		return fileNameTitle(file.Naam), receivedSubtitle(file)
	}
	docType := strings.ToLower(strings.TrimSpace(inv.DocumentType))
	if docType == claude.DocumentTypeOverig {
		reason := strings.TrimSpace(inv.DocumentTypeReason)
		if reason == "" {
			reason = "Geen factuur"
		}
		return fileNameTitle(file.Naam), reason + titleSeparator + file.Naam
	}

	supplier := strings.TrimSpace(inv.Leverancier)
	if supplier == "" {
		supplier = "Onbekende leverancier"
	}
	if docType == claude.DocumentTypeBonnetje {
		supplier = "Bonnetje " + supplier
	}
	parts := []string{supplier}
	if d, ok := parseISODate(inv.Datum); ok {
		parts = append(parts, formatDutchDate(d, true))
	}
	if inv.BedragInclBTW != 0 && !math.IsInf(inv.BedragInclBTW, 0) && !math.IsNaN(inv.BedragInclBTW) {
		parts = append(parts, formatMoney(inv.Currency, inv.BedragInclBTW))
	}
	title = strings.Join(parts, titleSeparator)

	number := strings.TrimSpace(inv.Factuurnummer)
	var kind string
	switch docType {
	case claude.DocumentTypeBonnetje:
		kind = "Bonnetje"
	case claude.DocumentTypeCreditnota:
		kind = "Creditnota"
		if number != "" {
			kind += " " + number
		}
	default:
		kind = "Factuur"
		if number != "" {
			kind += " " + number
		}
	}
	subParts := []string{kind, file.Naam}
	if eur, converted := convertToEUR(inv.BedragInclBTW, inv.Currency); converted && inv.BedragInclBTW != 0 {
		subParts = append(subParts, "≈ "+formatMoney("EUR", eur))
	}
	return title, strings.Join(subParts, titleSeparator)
}

// fileNameTitle turns a file name into a readable title: extension dropped,
// "_" and "-" replaced by spaces, whitespace collapsed.
func fileNameTitle(name string) string {
	base, _ := splitFileName(name)
	base = strings.NewReplacer("_", " ", "-", " ").Replace(base)
	title := strings.Join(strings.Fields(base), " ")
	if title == "" {
		title = strings.TrimSpace(name)
	}
	if title == "" {
		title = "Document"
	}
	return title
}

// receivedSubtitle is the pre-analysis subtitle: "Ontvangen 12 okt 14:32 ·
// PDF · 84 kB". The received part is left out when the date is unreadable.
func receivedSubtitle(file eboekhouden.ArchiveFile) string {
	var parts []string
	if t, ok := parseEBoekhoudenTime(file.Datum); ok {
		parts = append(parts, "Ontvangen "+formatDutchDate(t, false)+" "+t.Format("15:04"))
	}
	_, ext := splitFileName(file.Naam)
	parts = append(parts, fileTypeLabel(ext), formatFileSize(file.Size))
	return strings.Join(parts, titleSeparator)
}

// fileTypeLabel is the short type shown in the subtitle: PDF, JPG, PNG, or
// the uppercased extension for anything else ("Bestand" without one).
func fileTypeLabel(ext string) string {
	switch strings.ToLower(ext) {
	case "pdf":
		return "PDF"
	case "jpg", "jpeg":
		return "JPG"
	case "png":
		return "PNG"
	case "":
		return "Bestand"
	}
	return strings.ToUpper(ext)
}

// formatFileSize renders a size in kB (rounded, at least 1) below 1 MB and in
// MB with one decimal from 1 MB, both with a Dutch decimal comma. 1 kB is 1024
// bytes, as file managers show it.
func formatFileSize(size int64) string {
	const kB, mB = 1024.0, 1024.0 * 1024.0
	if float64(size) < mB {
		n := int64(math.Round(float64(size) / kB))
		if n < 1 {
			n = 1
		}
		return strconv.FormatInt(n, 10) + " kB"
	}
	s := strconv.FormatFloat(float64(size)/mB, 'f', 1, 64)
	return strings.Replace(s, ".", ",", 1) + " MB"
}

// dutchMonths are the month abbreviations used in titles.
var dutchMonths = [...]string{"jan", "feb", "mrt", "apr", "mei", "jun", "jul", "aug", "sep", "okt", "nov", "dec"}

// formatDutchDate renders "12 okt" or, with the year, "12 okt 2026".
func formatDutchDate(t time.Time, withYear bool) string {
	s := strconv.Itoa(t.Day()) + " " + dutchMonths[t.Month()-1]
	if withYear {
		s += " " + strconv.Itoa(t.Year())
	}
	return s
}

// eboekhoudenTimeLayouts are the forms the archive uses for file dates:
// "2006-01-02T15:04:05.99", the same without fraction, or a bare date.
var eboekhoudenTimeLayouts = []string{
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05",
	"2006-01-02",
}

// parseEBoekhoudenTime parses an e-boekhouden timestamp as the local wall
// time it represents, without any timezone conversion: the value has no zone
// and the result is only ever formatted, never compared to now.
func parseEBoekhoudenTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range eboekhoudenTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// formatNLAmount renders a number with two decimals in nl-NL style: a dot
// between thousands and a comma before the cents ("1.234,56", "-0,50").
func formatNLAmount(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "0,00"
	}
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	s := strconv.FormatFloat(v, 'f', 2, 64)
	intPart, frac := s[:len(s)-3], s[len(s)-2:]
	var b strings.Builder
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	return sign + b.String() + "," + frac
}

// formatMoney renders an amount with its currency: "€ 1.234,56" for EUR (or
// an unspecified currency), "CHF 32,80" for anything else.
func formatMoney(currency string, v float64) string {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" || currency == "EUR" {
		return "€ " + formatNLAmount(v)
	}
	return currency + " " + formatNLAmount(v)
}

// assignDocumentsToBankLines resolves competition between documents for the
// same bank lines. docs maps a document (archive file) id to its ranked
// candidates as produced by rankBankLineMatches. All (document, candidate)
// pairs are considered in one global order, best score first, and a pair is
// accepted when neither its document nor its bank line has been taken yet;
// a document that loses its best line therefore falls back to its next
// candidate if that line is still free.
//
// The order is fully deterministic: ties on score go to the lower document
// id, then to the lower bank line id. The result maps each assigned document
// id to the candidate it got; documents without a free candidate are absent.
//
// GET /documents uses this for the documents list, and the bank-line inbox
// (Step 5) uses the same assignment to annotate bank lines with the document
// that matches them, so both sides always agree.
func assignDocumentsToBankLines(docs map[int][]BankLineCandidate) map[int]BankLineCandidate {
	type pair struct {
		docID     int
		candidate BankLineCandidate
	}
	var pairs []pair
	for docID, cands := range docs {
		for _, cand := range cands {
			pairs = append(pairs, pair{docID: docID, candidate: cand})
		}
	}
	slices.SortFunc(pairs, func(a, b pair) int {
		if a.candidate.Score != b.candidate.Score {
			return cmp.Compare(b.candidate.Score, a.candidate.Score) // higher first
		}
		if a.docID != b.docID {
			return cmp.Compare(a.docID, b.docID)
		}
		return cmp.Compare(a.candidate.ID, b.candidate.ID)
	})

	assigned := make(map[int]BankLineCandidate)
	takenLines := make(map[int]bool)
	for _, p := range pairs {
		if _, done := assigned[p.docID]; done || takenLines[p.candidate.ID] {
			continue
		}
		assigned[p.docID] = p.candidate
		takenLines[p.candidate.ID] = true
	}
	return assigned
}
