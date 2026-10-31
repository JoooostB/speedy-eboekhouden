package handler

import (
	"math"
	"regexp"
	"strings"
	"time"
)

// Verification payments: the tiny amounts a merchant or a bank moves to check
// that a card or an account number is real. A card check (card_check) is a
// charge of at most € 2 with card wording in the description, usually paired
// with its refund a few days later under the same reference; an IBAN check
// (iban_check) is a cent-size transfer in either direction. Neither is a cost
// or revenue: the e-boekhouden way is to book them on "Bijzondere baten" with
// BTW GEEN, so Speedy decides them locally (no Claude, no learned lookup) and
// never learns them (the learned signal ignores the amount, so a learned
// "merchant -> 9300" would later swallow that merchant's real charges).
// Everything in this file is pure.

const (
	verificationKindCard = "card_check"
	verificationKindIBAN = "iban_check"
)

// Thresholds in cents.
const (
	cardPairMaxCents   = 200 // a card check with a refund partner
	cardSingleMaxCents = 100 // a card check without a partner in the inbox
	ibanCheckMaxCents  = 100 // an IBAN check is below one euro
	ibanFullConfCents  = 10  // up to this amount the IBAN check is certain
	cardPairMaxDays    = 3   // charge and refund at most this far apart
)

// verificationHit is what the detector knows about one bank line.
type verificationHit struct {
	Kind       string
	PairLineID int // the refund (or charge) of a card check; 0 for a single leg
	Confidence float64
	Category   string // "auto" or "review"
}

// inboxVerification is the verification field of an inboxClassifiedRow. It
// is deterministic (it only depends on the grid), so it may sit in the cached
// base.
type inboxVerification struct {
	Kind       string `json:"kind"`
	PairLineID int    `json:"pairLineId,omitempty"`
}

var (
	cardWords            = []string{"betaalpas", "ecom", "termbnet", "kaartnummer"}
	cardReferenceToken   = regexp.MustCompile(`^P\d{6,}$`)
	bijzondereBatenName  = regexp.MustCompile(`(?i)bijzondere\s+baten`)
	bijzondereBatenCode  = "9300"
	referenceTokenSplit  = regexp.MustCompile(`[\s,;]+`)
	referenceLettersOnly = regexp.MustCompile(`^[A-Za-z]+$`)
)

// hasCardWording reports whether a bank description reads like a card
// transaction as Dutch banks print them.
func hasCardWording(omschrijving string) bool {
	s := strings.ToLower(omschrijving)
	for _, w := range cardWords {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// cardReference is the token that ties a card charge to its refund: the
// first "P" plus at least six digits, else the first token of at least six
// characters that is not pure letters and not a date. Empty when none.
func cardReference(omschrijving string) string {
	tokens := referenceTokenSplit.Split(omschrijving, -1)
	for _, t := range tokens {
		if cardReferenceToken.MatchString(t) {
			return t
		}
	}
	for _, t := range tokens {
		if len(t) < 6 || referenceLettersOnly.MatchString(t) || strings.ContainsAny(t, "./") {
			continue
		}
		return t
	}
	return ""
}

// verificationCents is |bedrag| in whole cents.
func verificationCents(bedrag float64) int64 {
	return int64(math.Round(math.Abs(bedrag) * 100))
}

// daysApart is the distance between two grid dates ("2026-10-23T00:00:00");
// an unparseable date counts as far apart.
func daysApart(a, b string) int {
	ta, errA := time.Parse("2006-01-02", firstN(a, 10))
	tb, errB := time.Parse("2006-01-02", firstN(b, 10))
	if errA != nil || errB != nil {
		return math.MaxInt32
	}
	d := int(math.Abs(ta.Sub(tb).Hours()) / 24)
	return d
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ibanCheckConfidence is 1 up to ten cents and falls linearly to 0.6 at 99
// cents, rounded to two decimals.
func ibanCheckConfidence(c int64) float64 {
	if c <= ibanFullConfCents {
		return 1
	}
	conf := 1 - 0.4*float64(c-ibanFullConfCents)/float64(ibanCheckMaxCents-1-ibanFullConfCents)
	return math.Round(conf*100) / 100
}

// detectVerificationPayments finds the card and IBAN checks among the
// unprocessed lines, keyed by line id. Card pairs are matched greedily in
// id order; a line is in at most one pair.
func detectVerificationPayments(lines []bankLine) map[int]verificationHit {
	hits := make(map[int]verificationHit)
	paired := make(map[int]bool)

	for i, a := range lines {
		if paired[a.ID] || !hasCardWording(a.Omschrijving) {
			continue
		}
		ca := verificationCents(a.Bedrag)
		if ca == 0 || ca > cardPairMaxCents {
			continue
		}
		ref := cardReference(a.Omschrijving)
		if ref == "" {
			continue
		}
		for _, b := range lines[i+1:] {
			if paired[b.ID] || b.ID == a.ID || !hasCardWording(b.Omschrijving) {
				continue
			}
			if verificationCents(b.Bedrag) != ca || (a.Bedrag < 0) == (b.Bedrag < 0) {
				continue
			}
			if cardReference(b.Omschrijving) != ref || daysApart(a.Datum, b.Datum) > cardPairMaxDays {
				continue
			}
			hits[a.ID] = verificationHit{Kind: verificationKindCard, PairLineID: b.ID, Confidence: 1, Category: "auto"}
			hits[b.ID] = verificationHit{Kind: verificationKindCard, PairLineID: a.ID, Confidence: 1, Category: "auto"}
			paired[a.ID], paired[b.ID] = true, true
			break
		}
	}

	for _, l := range lines {
		if paired[l.ID] {
			continue
		}
		c := verificationCents(l.Bedrag)
		if c == 0 {
			continue
		}
		if hasCardWording(l.Omschrijving) {
			if c <= cardSingleMaxCents {
				hits[l.ID] = verificationHit{Kind: verificationKindCard, Confidence: 1, Category: "auto"}
			}
			continue
		}
		if c < ibanCheckMaxCents {
			conf := ibanCheckConfidence(c)
			cat := "review"
			if conf >= 0.9 {
				cat = "auto"
			}
			hits[l.ID] = verificationHit{Kind: verificationKindIBAN, Confidence: conf, Category: cat}
		}
	}
	return hits
}

// ledgerAccount is the code and name of the account verification payments
// are booked on, as actieveGBR lists it.
type ledgerAccount struct {
	Code string
	Name string
}

// label is "9300 Bijzondere baten", the way the UI names an account.
func (a ledgerAccount) label() string {
	return strings.TrimSpace(a.Code + " " + a.Name)
}

// findBijzondereBaten finds the ledger account verification payments are
// booked on. actieveGBR has no category that identifies it (every P&L
// account is "VW"), so the name decides, with code 9300 as the fallback
// (whatever that account is called).
func findBijzondereBaten(accs []map[string]any) (ledgerAccount, bool) {
	var fallback ledgerAccount
	for _, acc := range accs {
		code, _ := acc["code"].(string)
		name, _ := acc["omschrijving"].(string)
		if code == "" {
			continue
		}
		if bijzondereBatenName.MatchString(name) {
			return ledgerAccount{Code: code, Name: strings.TrimSpace(name)}, true
		}
		if code == bijzondereBatenCode {
			fallback = ledgerAccount{Code: code, Name: strings.TrimSpace(name)}
		}
	}
	return fallback, fallback.Code != ""
}

// verificationIndicator is the row caption. User-facing Dutch.
func verificationIndicator(hit verificationHit, acc ledgerAccount) string {
	if acc.Code == "" {
		return "Controlebetaling. Geen rekening Bijzondere baten gevonden: kies zelf een grootboekrekening."
	}
	if hit.Kind == verificationKindCard && hit.PairLineID == 0 {
		return "Controlebetaling zonder terugboeking in de inbox · boeken op " + acc.label()
	}
	return "Controlebetaling · boeken op " + acc.label()
}

// withVerification fills a base row from a detector hit. Without a ledger
// account the suggestion is incomplete, so the row is "review" and the user
// picks the account.
func withVerification(row inboxClassifiedRow, hit verificationHit, acc ledgerAccount) inboxClassifiedRow {
	row.Category = hit.Category
	if acc.Code == "" {
		row.Category = "review"
	}
	row.NeedsInvoice = false
	row.Confidence = hit.Confidence
	row.Soort = "GeldOntvangen"
	if row.Bedrag < 0 {
		row.Soort = "GeldUitgegeven"
	}
	row.BTWCode = "GEEN"
	row.Grootboekcode = acc.Code
	row.AIOmschrijving = row.Omschrijving
	row.Indicator = verificationIndicator(hit, acc)
	row.Learned = false
	row.Verification = &inboxVerification{Kind: hit.Kind, PairLineID: hit.PairLineID}
	return row
}
