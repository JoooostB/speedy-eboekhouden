package handler

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/joooostb/speedy-eboekhouden/internal/claude"
)

// maxBankLineCandidates caps how many ranked bank lines an analysis returns.
// The UI offers these in a select; more than three is noise.
const maxBankLineCandidates = 3

// BankLineCandidate is one unprocessed bank line that could be the payment
// of an analysed document (or, for a credit note, the refund). The JSON tags
// match the legacy matchedBankLine object so the frontend can treat the
// first candidate and matchedBankLine interchangeably.
type BankLineCandidate struct {
	ID           int     `json:"id"`
	Datum        string  `json:"datum"`
	Bedrag       float64 `json:"bedrag"`
	Omschrijving string  `json:"omschrijving"`
	// CurrencyConverted is true when the invoice is non-EUR and the amount
	// test went through convertToEUR. InvoiceCurrency and InvoiceAmount then
	// carry the original values so the UI can show "32.80 CHF ≈ € 34.50".
	CurrencyConverted bool    `json:"currencyConverted"`
	InvoiceCurrency   string  `json:"invoiceCurrency"`
	InvoiceAmount     float64 `json:"invoiceAmount"`
	// Score orders candidates; see rankBankLineMatches for the formula.
	Score float64 `json:"score"`
}

// Score weights for rankBankLineMatches.
//
//	score = amount + date + name
//
//	amount, EUR invoice:  100 − 2500·|Δ€|              → (50, 100]; exact = 100, 1 ct off = 75
//	amount, non-EUR:      40·(1 − relΔ/currencyMatchTolerance) → (0, 40]; bank amount equal to the FX estimate = 40
//	date:                 20·(1 − |Δdays|/90)          → only when the bank date is in [invoice − 7 d, invoice + 90 d], else 0
//	name:                 10                           → a supplier token appears as a whole word in the bank description
//
// The amount term dominates: an exact EUR match scores at least 100, while
// the best possible FX match tops out at 40 + 20 + 10 = 70. That matters
// when several documents compete for one bank line (the document inbox
// assigns lines greedily by score). Within one document every candidate
// passes the same amount test, so date and name decide between lines with
// the same amount.
const (
	eurAmountScoreMax  = 100.0
	eurAmountTolerance = 0.02
	fxAmountScoreMax   = 40.0
	dateScoreMax       = 20.0
	dateWindowBefore   = 7  // days before the invoice date that still earn a bonus
	dateWindowAfter    = 90 // days after the invoice date that still earn a bonus
	nameScore          = 10.0
)

// bankLineMatchable reports whether a document can be matched to a bank
// line at all: it must be a factuur, bonnetje or creditnota with a positive
// total. Callers use it to skip fetching the import grid for documents that
// can never match.
func bankLineMatchable(inv *claude.InvoiceData) bool {
	if inv == nil {
		return false
	}
	if strings.ToLower(strings.TrimSpace(inv.DocumentType)) == claude.DocumentTypeOverig {
		return false
	}
	return inv.BedragInclBTW > 0 && !math.IsInf(inv.BedragInclBTW, 0)
}

// assignsBankLine reports whether inv takes part in bank-line matching for
// booking: the analysis response, the documents list and the "Document
// gevonden" annotation. A creditnota does not for now, because it cannot be
// booked (errCreditnotaNotSupported) and pairing it with an incoming line
// would only lead the user to a refused booking. rankBankLineMatches still
// knows how to rank one, for when credit notes can be booked.
func assignsBankLine(inv *claude.InvoiceData) bool {
	return bankLineMatchable(inv) && strings.ToLower(strings.TrimSpace(inv.DocumentType)) != claude.DocumentTypeCreditnota
}

// rankBankLineMatches returns the unprocessed bank lines that could belong
// to inv, best first, at most maxBankLineCandidates. rows are import-grid
// rows as produced by eboekhouden.ParseImportGrid; ids in exclude are
// skipped (lines already claimed by another document).
//
// Eligibility: outgoing lines (mutBedrag < 0) for a factuur or bonnetje,
// incoming lines (mutBedrag > 0) for a creditnota, compared on the absolute
// bank amount. Credit notes carry positive amounts; the sign lives in
// DocumentType. "overig", zero amounts and rows without a usable id never
// match. The amount test keeps the historical tolerances: within €0,02 for
// EUR, within currencyMatchTolerance of the convertToEUR estimate otherwise.
//
// The result is sorted by score descending with bank id ascending as the
// tie-break, so the ranking is deterministic. It is never nil.
func rankBankLineMatches(inv *claude.InvoiceData, rows []map[string]any, exclude map[int]bool) []BankLineCandidate {
	out := []BankLineCandidate{}
	if !bankLineMatchable(inv) {
		return out
	}

	amount := inv.BedragInclBTW
	wantIncoming := strings.ToLower(strings.TrimSpace(inv.DocumentType)) == claude.DocumentTypeCreditnota
	eurEstimate, fxConverted := convertToEUR(amount, inv.Currency)
	if !(eurEstimate > 0) || math.IsInf(eurEstimate, 0) {
		return out
	}
	currency := strings.ToUpper(strings.TrimSpace(inv.Currency))
	invoiceDate, hasInvoiceDate := parseISODate(inv.Datum)
	supplierTokens := supplierNameTokens(inv.Leverancier)

	for _, row := range rows {
		id, _ := toInt(row["id"])
		if id <= 0 || exclude[id] {
			continue
		}
		bedrag, _ := toFloat(row["mutBedrag"])
		if bedrag == 0 || (bedrag > 0) != wantIncoming {
			continue
		}
		absBedrag := abs(bedrag)

		var score float64
		if !fxConverted {
			diff := abs(absBedrag - amount)
			if diff >= eurAmountTolerance {
				continue
			}
			score = eurAmountScoreMax - (eurAmountScoreMax/2)*(diff/eurAmountTolerance)
		} else {
			rel := abs(absBedrag-eurEstimate) / eurEstimate
			if rel >= currencyMatchTolerance {
				continue
			}
			score = fxAmountScoreMax * (1 - rel/currencyMatchTolerance)
		}

		datum := toString(row["mutDatum"])
		if hasInvoiceDate {
			if bankDate, ok := parseISODate(datum); ok {
				score += dateScore(invoiceDate, bankDate)
			}
		}

		omschrijving := toString(row["mutOmschrijving"])
		if descriptionMentions(omschrijving, supplierTokens) {
			score += nameScore
		}

		out = append(out, BankLineCandidate{
			ID:                id,
			Datum:             datum,
			Bedrag:            bedrag,
			Omschrijving:      omschrijving,
			CurrencyConverted: fxConverted,
			InvoiceCurrency:   currency,
			InvoiceAmount:     amount,
			Score:             score,
		})
	}

	slices.SortFunc(out, func(a, b BankLineCandidate) int {
		if a.Score != b.Score {
			return cmp.Compare(b.Score, a.Score) // higher score first
		}
		return cmp.Compare(a.ID, b.ID)
	})
	if len(out) > maxBankLineCandidates {
		out = out[:maxBankLineCandidates]
	}
	return out
}

// legacyJSON renders the candidate in the matchedBankLine shape the frontend
// has always received (no score).
func (bc BankLineCandidate) legacyJSON() map[string]any {
	return map[string]any{
		"id":                bc.ID,
		"datum":             bc.Datum,
		"bedrag":            bc.Bedrag,
		"omschrijving":      bc.Omschrijving,
		"currencyConverted": bc.CurrencyConverted,
		"invoiceCurrency":   bc.InvoiceCurrency,
		"invoiceAmount":     bc.InvoiceAmount,
	}
}

// dateScore is the date term of the score: dateScoreMax on the invoice date,
// decaying linearly to 0 at dateWindowAfter days distance, and 0 outside the
// [−dateWindowBefore, +dateWindowAfter] window. Both inputs are midnight
// dates from parseISODate.
func dateScore(invoiceDate, bankDate time.Time) float64 {
	days := int(math.Round(bankDate.Sub(invoiceDate).Hours() / 24))
	if days < -dateWindowBefore || days > dateWindowAfter {
		return 0
	}
	if days < 0 {
		days = -days
	}
	return dateScoreMax * (1 - float64(days)/float64(dateWindowAfter))
}

// parseISODate reads the calendar date at the start of s. The import grid
// uses "2006-01-02T15:04:05" (sometimes with fractional seconds) and Claude
// returns "2006-01-02"; both begin with the ISO date, so only the first ten
// bytes are parsed. Anything else yields ok=false and never panics.
func parseISODate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if len(s) < 10 {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", s[:10])
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// legalFormTokens are company-suffix words that say nothing about who the
// supplier is. They never count as a name hit: "Acme B.V." must not match a
// bank line that mentions some other B.V. Tokens shorter than three
// characters (bv, nv, ag, co, …) are already dropped by the length rule;
// they are listed anyway so the intent is visible in one place.
var legalFormTokens = map[string]bool{
	"bv": true, "nv": true, "vof": true, "cv": true,
	"inc": true, "incorporated": true, "ltd": true, "limited": true, "llc": true, "plc": true, "corp": true, "co": true,
	"gmbh": true, "ag": true, "kg": true, "ug": true,
	"pbc": true, "sa": true, "sarl": true, "sas": true, "srl": true, "spa": true, "se": true,
	"ab": true, "aps": true, "oy": true,
}

// normalizeTokens lowercases s, treats every character that is not a letter
// or digit as a separator and returns the resulting words.
func normalizeTokens(s string) []string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.Fields(b.String())
}

// supplierNameTokens returns the distinctive words of a supplier name: at
// least three characters and not a legal-form suffix.
func supplierNameTokens(name string) []string {
	var out []string
	for _, tok := range normalizeTokens(name) {
		if utf8.RuneCountInString(tok) < 3 || legalFormTokens[tok] {
			continue
		}
		out = append(out, tok)
	}
	return out
}

// descriptionMentions reports whether any supplier token occurs as a whole
// word in the bank description.
func descriptionMentions(omschrijving string, supplierTokens []string) bool {
	if len(supplierTokens) == 0 {
		return false
	}
	words := normalizeTokens(omschrijving)
	for _, tok := range supplierTokens {
		if slices.Contains(words, tok) {
			return true
		}
	}
	return false
}
