package handler

import "strings"

// fxRatesToEUR holds approximate EUR-equivalent exchange rates for the
// currencies a Dutch ZZP/B.V. is most likely to encounter on a foreign
// invoice or receipt. These are deliberately approximate — they're only
// used to FUZZY-MATCH a non-EUR invoice to the EUR bank line that paid
// for it. The actual booking always uses the bank line's real EUR amount,
// so the precision of the rate here doesn't affect any accounting figures.
//
// Updating this table is fine but not urgent — the match tolerance
// (currencyMatchTolerance) is wide enough to absorb several percent of
// drift, plus the FX margin the user's bank/card processor applies on top.
//
// If we ever want live rates, the European Central Bank's reference rates
// (ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml) are free, no auth,
// updated daily. Cache for 24h and fall back to this table on failure.
var fxRatesToEUR = map[string]float64{
	"EUR": 1.0,
	"CHF": 1.05, // 1 CHF ≈ 1.05 EUR (Swiss franc usually a bit above EUR)
	"USD": 0.92, // 1 USD ≈ 0.92 EUR
	"GBP": 1.17, // 1 GBP ≈ 1.17 EUR
	"DKK": 0.13, // 1 DKK ≈ 0.13 EUR
	"SEK": 0.087,
	"NOK": 0.085,
	"JPY": 0.0061,
	"CAD": 0.67,
	"AUD": 0.61,
	"PLN": 0.23,
	"CZK": 0.040,
	"NZD": 0.55,
}

// convertToEUR returns the approximate EUR value of `amount` denominated
// in `currency`, plus a flag indicating whether a non-trivial conversion
// was applied. Unknown currencies fall through with `converted=false` so
// the caller can refuse to fuzzy-match them.
func convertToEUR(amount float64, currency string) (float64, bool) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" || currency == "EUR" {
		return amount, false
	}
	rate, ok := fxRatesToEUR[currency]
	if !ok {
		return amount, false
	}
	return amount * rate, true
}

// currencyMatchTolerance is the relative tolerance for fuzzy bank-line
// matching when the invoice is in a non-EUR currency. 12% covers the
// approximate-rate drift in fxRatesToEUR (typically ~3-5%) plus the FX
// margin that consumer banks and card networks add on top (typically
// 1-3% combined), with comfortable headroom. Wider than ideal but matching
// is best-effort and false positives are easy to spot — the user reviews
// every match in the dialog before booking.
const currencyMatchTolerance = 0.12
