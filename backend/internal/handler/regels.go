package handler

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// A split booking: one mutation with several mutatieRegels, each on its own
// tegenrekening with its own BTW code and amount. This file is the single
// line model behind SubmitFull (soort 1) and SubmitReceipt (soort 6); the
// bank-line split (BatchProcess, soort 5/6) reuses it with a grootboekcode
// resolver.
//
// The shape is what the e-boekhouden web UI sends in "Incl btw" mode for a
// "Factuur ontvangen" with two regels: inEx "IN" on the header, no total on
// the header, and per regel index 0..n-1, its own tegenRekening, bedrag =
// the gross amount, bedragInclusief = that same amount, bedragExclusief and
// btw as the UI rounds them per regel (excl = round(incl / (1 + rate)), btw
// = incl - excl; regelExclCents), the same btwCode allowed on several regels
// and no per-regel omschrijving on an invoice. The regels' BTW may therefore
// differ a cent from the invoice's own BTW line; that is how e-boekhouden
// books it itself, so Speedy does not spread cents. "IN" with the gross
// bedrag matters: in "EX" mode the server may recompute btw = round(excl ×
// rate), which can leave a cent open on the invoice. The legacy single-line
// paths keep their own "EX" + net bedrag convention untouched.

// maxRegels caps the lines of one mutation. The UI offers fewer; the limit
// only keeps a client from sending a mutation e-boekhouden would refuse.
const maxRegels = 10

// regelInput is one booking line as the client sends it. The tegenrekening
// is either its internal id (tegenRekeningId, the review dialog) or its code
// (grootboekcode, resolved by the caller; the bank-line split). bedragIncl is
// the gross amount of the line; its BTW follows from btwCode.
type regelInput struct {
	Omschrijving    string  `json:"omschrijving,omitempty"`
	TegenRekeningId int     `json:"tegenRekeningId,omitempty"`
	Grootboekcode   string  `json:"grootboekcode,omitempty"`
	BTWCode         string  `json:"btwCode,omitempty"`
	BedragIncl      float64 `json:"bedragIncl"`
}

// regel is a validated line with its BTW split.
type regel struct {
	Omschrijving  string
	TegenRekening int
	BTWCode       string
	BedragIncl    float64
	BedragExcl    float64
	BTW           float64
}

// ledgerResolver turns a grootboekcode into its internal id. It is nil when
// the caller only accepts tegenRekeningId (SubmitFull, SubmitReceipt).
type ledgerResolver func(code string) (int, bool)

var errNoRegels = errors.New("Een boeking heeft minstens één regel nodig.")

// normalizeRegels validates the lines of a split booking and splits each by
// its BTW code. total is the amount the lines must add up to (the invoice
// total, or the bank line for a receipt), within a cent. An empty BTW code
// is GEEN; every code is normalised for the soort like a single-line booking.
// Errors are user-facing Dutch and name the line ("Regel 2: …").
func normalizeRegels(in []regelInput, total float64, soort int, resolve ledgerResolver) ([]regel, error) {
	if len(in) == 0 {
		return nil, errNoRegels
	}
	if len(in) > maxRegels {
		return nil, fmt.Errorf("Een boeking kan maximaal %d regels hebben.", maxRegels)
	}
	if math.IsNaN(total) || math.IsInf(total, 0) || total <= 0 {
		return nil, errors.New("Het totaal moet groter zijn dan nul.")
	}
	out := make([]regel, 0, len(in))
	var sumCents int64
	for i, r := range in {
		n := i + 1
		if math.IsNaN(r.BedragIncl) || math.IsInf(r.BedragIncl, 0) {
			return nil, fmt.Errorf("Regel %d: het bedrag is ongeldig.", n)
		}
		incl := roundCents(r.BedragIncl)
		if incl <= 0 {
			return nil, fmt.Errorf("Regel %d: het bedrag moet groter zijn dan nul.", n)
		}
		tegen := r.TegenRekeningId
		if tegen == 0 && resolve != nil && strings.TrimSpace(r.Grootboekcode) != "" {
			if id, ok := resolve(strings.TrimSpace(r.Grootboekcode)); ok {
				tegen = id
			} else {
				return nil, fmt.Errorf("Regel %d: grootboekrekening %s bestaat niet in je administratie.", n, strings.TrimSpace(r.Grootboekcode))
			}
		}
		if tegen <= 0 {
			return nil, fmt.Errorf("Regel %d: kies een grootboekrekening.", n)
		}
		code := strings.TrimSpace(r.BTWCode)
		if code == "" {
			code = "GEEN"
		}
		code = normalizeBTWForSoort(code, soort)
		excl, btw := regelBTW(incl, code)
		out = append(out, regel{
			Omschrijving:  strings.TrimSpace(r.Omschrijving),
			TegenRekening: tegen,
			BTWCode:       code,
			BedragIncl:    incl,
			BedragExcl:    excl,
			BTW:           btw,
		})
		sumCents += cents(incl)
	}
	if diff := sumCents - cents(total); diff > 1 || diff < -1 {
		return nil, fmt.Errorf("De regels tellen op tot %s, het totaal is %s.",
			formatMoney("EUR", float64(sumCents)/100), formatMoney("EUR", roundCents(total)))
	}
	return out, nil
}

// regelBTW splits a gross line amount by its BTW code the way the
// e-boekhouden web UI does per regel: the net amount is rounded first and
// the BTW is the remainder, so excl + btw == incl holds for every line.
// The rounding runs in whole cents with integer arithmetic (regelExclCents)
// so the frontend's regelBtwCents gives the same answer for every amount.
func regelBTW(incl float64, btwCode string) (excl, btw float64) {
	inclCents := cents(incl)
	exclCents := regelExclCents(inclCents, btwRateFromCode(btwCode))
	return float64(exclCents) / 100, float64(inclCents-exclCents) / 100
}

// regelExclCents is the net amount of a gross amount in cents at a BTW rate
// in percent: round-half-away-from-zero of incl / (1 + rate/100), computed
// as integers in basis points. floor((2a + d) / 2d) is round(a / d) for
// a >= 0. A non-positive amount is returned as is.
func regelExclCents(inclCents int64, rate float64) int64 {
	if inclCents <= 0 || rate <= 0 {
		return inclCents
	}
	denom := 10000 + int64(math.Round(rate*100))
	return (2*inclCents*10000 + denom) / (2 * denom)
}

// mutatieRegelMaps renders validated lines as mutatieRegels with index
// 0..n-1 in the UI's "Incl btw" shape: bedrag is the gross amount (the
// caller sends inEx "IN" on the mutation), next to the per-regel split.
// withOmschrijving adds the line's own description when it has one: a
// receipt names its lines ("Fooi"), an invoice does not, as in the
// e-boekhouden UI's own request.
func mutatieRegelMaps(regels []regel, withOmschrijving bool) []map[string]any {
	out := make([]map[string]any, 0, len(regels))
	for i, r := range regels {
		m := map[string]any{
			"index":           i,
			"bedrag":          r.BedragIncl,
			"tegenRekening":   r.TegenRekening,
			"bedragExclusief": r.BedragExcl,
			"bedragInclusief": r.BedragIncl,
			"btwCode":         r.BTWCode,
			"btw":             r.BTW,
		}
		if withOmschrijving && r.Omschrijving != "" {
			m["omschrijving"] = truncate(r.Omschrijving, 200)
		}
		out = append(out, m)
	}
	return out
}
