package handler

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/joooostb/speedy-eboekhouden/internal/claude"
)

// bankRow builds an import-grid row the way ParseImportGrid does: numbers
// arrive as float64, dates as "2006-01-02T15:04:05" strings.
func bankRow(id int, datum string, bedrag float64, omschrijving string) map[string]any {
	return map[string]any{
		"id":              float64(id),
		"mutDatum":        datum,
		"mutBedrag":       bedrag,
		"mutOmschrijving": omschrijving,
	}
}

func testInvoice(docType string, amount float64, currency, datum, leverancier string) *claude.InvoiceData {
	return &claude.InvoiceData{
		DocumentType:  docType,
		BedragInclBTW: amount,
		Currency:      currency,
		Datum:         datum,
		Leverancier:   leverancier,
	}
}

func candidateIDs(cands []BankLineCandidate) []int {
	ids := make([]int, 0, len(cands))
	for _, c := range cands {
		ids = append(ids, c.ID)
	}
	return ids
}

func TestRankBankLineMatches(t *testing.T) {
	const day = "2026-10-12T00:00:00"
	same := func(ids ...int) []map[string]any {
		rows := make([]map[string]any, 0, len(ids))
		for _, id := range ids {
			rows = append(rows, bankRow(id, day, -21.78, ""))
		}
		return rows
	}

	tests := []struct {
		name     string
		inv      *claude.InvoiceData
		rows     []map[string]any
		exclude  map[int]bool
		want     []int // expected ids, best first
		anyOrder bool  // set when the order between equals is not part of the contract
	}{
		{
			name: "exact EUR hit",
			inv:  testInvoice("factuur", 21.78, "EUR", "", ""),
			rows: []map[string]any{bankRow(1, day, -21.78, ""), bankRow(2, day, -30.00, "")},
			want: []int{1},
		},
		{
			name:     "one cent off still hits",
			inv:      testInvoice("factuur", 21.78, "EUR", "", ""),
			rows:     []map[string]any{bankRow(1, day, -21.77, ""), bankRow(2, day, -21.79, "")},
			want:     []int{1, 2},
			anyOrder: true,
		},
		{
			name: "three cents off misses",
			inv:  testInvoice("factuur", 21.78, "EUR", "", ""),
			rows: []map[string]any{bankRow(1, day, -21.75, ""), bankRow(2, day, -21.81, "")},
			want: []int{},
		},
		{
			// 32.80 CHF ≈ 34.44 EUR: 34.50 is 0.2% off, 40.00 is 16% off.
			name: "FX hit within the currency tolerance",
			inv:  testInvoice("factuur", 32.80, "CHF", "", ""),
			rows: []map[string]any{bankRow(1, day, -34.50, ""), bankRow(2, day, -40.00, "")},
			want: []int{1},
		},
		{
			// convertToEUR refuses unknown currencies, so the tight EUR test applies.
			name: "unknown currency is not fuzzy-matched",
			inv:  testInvoice("factuur", 32.80, "XYZ", "", ""),
			rows: []map[string]any{bankRow(1, day, -34.50, ""), bankRow(2, day, -32.80, "")},
			want: []int{2},
		},
		{
			name: "incoming lines are ignored for a factuur",
			inv:  testInvoice("factuur", 21.78, "EUR", "", ""),
			rows: []map[string]any{bankRow(1, day, 21.78, "")},
			want: []int{},
		},
		{
			name: "a bonnetje matches outgoing lines like a factuur",
			inv:  testInvoice("bonnetje", 21.78, "EUR", "", ""),
			rows: []map[string]any{bankRow(1, day, -21.78, ""), bankRow(2, day, 21.78, "")},
			want: []int{1},
		},
		{
			// Credit notes carry positive amounts; only the direction flips.
			name: "a creditnota matches an incoming line",
			inv:  testInvoice("creditnota", 21.78, "EUR", "", ""),
			rows: []map[string]any{bankRow(1, day, 21.78, ""), bankRow(2, day, -21.78, "")},
			want: []int{1},
		},
		{
			name: "overig never matches",
			inv:  testInvoice("overig", 21.78, "EUR", "", ""),
			rows: []map[string]any{bankRow(1, day, -21.78, "")},
			want: []int{},
		},
		{
			name: "zero amount never matches",
			inv:  testInvoice("factuur", 0, "EUR", "", ""),
			rows: []map[string]any{bankRow(1, day, -0.01, ""), bankRow(2, day, 0, "")},
			want: []int{},
		},
		{
			name: "empty documentType is treated as factuur",
			inv:  testInvoice("", 21.78, "EUR", "", ""),
			rows: []map[string]any{bankRow(1, day, -21.78, "")},
			want: []int{1},
		},
		{
			name: "equal amounts: the closer date wins, outside the window comes last",
			inv:  testInvoice("factuur", 21.78, "EUR", "2026-10-10", ""),
			rows: []map[string]any{
				bankRow(1, "2026-12-01T00:00:00", -21.78, ""), // +52 days
				bankRow(2, "2026-10-12T00:00:00", -21.78, ""), // +2 days
				bankRow(3, "2026-03-01T00:00:00", -21.78, ""), // far before: no bonus
			},
			want: []int{2, 1, 3},
		},
		{
			name: "a payment shortly before the invoice date still earns the date bonus",
			inv:  testInvoice("factuur", 21.78, "EUR", "2026-10-10", ""),
			rows: []map[string]any{
				bankRow(1, "2026-09-20T00:00:00", -21.78, ""), // −20 days: outside
				bankRow(2, "2026-10-05T00:00:00", -21.78, ""), // −5 days: inside
			},
			want: []int{2, 1},
		},
		{
			name: "date-only mutDatum is parsed too",
			inv:  testInvoice("factuur", 21.78, "EUR", "2026-10-10", ""),
			rows: []map[string]any{bankRow(1, "2026-12-01", -21.78, ""), bankRow(2, "2026-10-11", -21.78, "")},
			want: []int{2, 1},
		},
		{
			name: "equal amounts and dates: the supplier name decides",
			inv:  testInvoice("factuur", 21.78, "EUR", "2026-10-10", "Anthropic, PBC"),
			rows: []map[string]any{
				bankRow(1, day, -21.78, "SEPA Overboeking Foo Shop"),
				bankRow(2, day, -21.78, "ANTHROPIC PBC SAN FRANCISCO"),
			},
			want: []int{2, 1},
		},
		{
			name: "a supplier token must match a whole word",
			inv:  testInvoice("factuur", 21.78, "EUR", "", "Shell"),
			rows: []map[string]any{
				bankRow(1, day, -21.78, "Shellfish Market"),
				bankRow(2, day, -21.78, "Shell Station 1234"),
			},
			want: []int{2, 1},
		},
		{
			name: "a legal-form token is not a name hit",
			inv:  testInvoice("factuur", 21.78, "EUR", "", "Acme B.V."),
			rows: []map[string]any{
				bankRow(2, day, -21.78, "Betaling aan Zonnig B.V."),
				bankRow(1, day, -21.78, "Betaling iets anders"),
			},
			want: []int{1, 2}, // no bonus either way, so id order
		},
		{
			name:    "exclude is honoured",
			inv:     testInvoice("factuur", 21.78, "EUR", "", ""),
			rows:    same(1, 2),
			exclude: map[int]bool{1: true},
			want:    []int{2},
		},
		{
			name: "at most three candidates",
			inv:  testInvoice("factuur", 21.78, "EUR", "", ""),
			rows: same(1, 2, 3, 4, 5),
			want: []int{1, 2, 3},
		},
		{
			name: "ties are broken by id regardless of input order",
			inv:  testInvoice("factuur", 21.78, "EUR", "", ""),
			rows: same(5, 3, 9, 1),
			want: []int{1, 3, 5},
		},
		{
			name: "unparseable bank dates do not panic and earn no bonus",
			inv:  testInvoice("factuur", 21.78, "EUR", "2026-10-10", ""),
			rows: []map[string]any{
				bankRow(1, "", -21.78, ""),
				bankRow(2, "2026", -21.78, ""),
				bankRow(3, "24-10-2026", -21.78, ""),
				bankRow(4, "2026-13-45T00:00:00", -21.78, ""),
				bankRow(5, "2026-10-11T00:00:00", -21.78, ""), // the only valid date wins
			},
			want: []int{5, 1, 2},
		},
		{
			name: "an unparseable invoice date disables the date bonus",
			inv:  testInvoice("factuur", 21.78, "EUR", "garbage", ""),
			rows: []map[string]any{bankRow(2, "2026-10-11T00:00:00", -21.78, ""), bankRow(1, "2027-01-01T00:00:00", -21.78, "")},
			want: []int{1, 2},
		},
		{
			name: "rows without a usable id are skipped",
			inv:  testInvoice("factuur", 21.78, "EUR", "", ""),
			rows: []map[string]any{{"mutBedrag": -21.78}, bankRow(0, day, -21.78, ""), bankRow(7, day, -21.78, "")},
			want: []int{7},
		},
		{
			name: "nil invoice and nil rows yield an empty result",
			inv:  nil,
			rows: nil,
			want: []int{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rankBankLineMatches(tc.inv, tc.rows, tc.exclude)
			if got == nil {
				t.Fatal("result is nil; it must be an empty slice so the JSON renders []")
			}
			ids := candidateIDs(got)
			want := tc.want
			if tc.anyOrder {
				slices.Sort(ids)
				want = slices.Clone(tc.want)
				slices.Sort(want)
			}
			if !slices.Equal(ids, want) {
				t.Fatalf("ids = %v, want %v\ncandidates: %+v", ids, want, got)
			}
			for i := 1; i < len(got); i++ {
				if got[i-1].Score < got[i].Score {
					t.Fatalf("not sorted by score descending: %+v", got)
				}
			}
		})
	}
}

func TestRankBankLineMatchesCandidateFields(t *testing.T) {
	fx := rankBankLineMatches(
		testInvoice("factuur", 32.80, "chf", "", ""),
		[]map[string]any{bankRow(1, "2026-10-12T00:00:00", -34.50, "TESLA SUPERCHARGER")},
		nil,
	)
	if len(fx) != 1 {
		t.Fatalf("got %d candidates, want 1", len(fx))
	}
	c := fx[0]
	if c.ID != 1 || c.Datum != "2026-10-12T00:00:00" || c.Bedrag != -34.50 || c.Omschrijving != "TESLA SUPERCHARGER" {
		t.Errorf("bank line fields not copied: %+v", c)
	}
	if !c.CurrencyConverted || c.InvoiceCurrency != "CHF" || c.InvoiceAmount != 32.80 {
		t.Errorf("FX fields wrong: %+v", c)
	}

	eur := rankBankLineMatches(testInvoice("factuur", 21.78, "EUR", "", ""), []map[string]any{bankRow(2, "", -21.78, "")}, nil)
	if len(eur) != 1 {
		t.Fatalf("got %d candidates, want 1", len(eur))
	}
	if eur[0].CurrencyConverted || eur[0].InvoiceCurrency != "EUR" || eur[0].InvoiceAmount != 21.78 {
		t.Errorf("EUR fields wrong: %+v", eur[0])
	}
}

// An exact EUR match must beat the best possible FX match (amount equal to
// the estimate, same day, supplier named) because the document inbox assigns
// bank lines to documents by score.
func TestExactEURMatchOutranksAnyFXMatch(t *testing.T) {
	fx := rankBankLineMatches(
		testInvoice("factuur", 100, "USD", "2026-10-10", "Anthropic"),
		[]map[string]any{bankRow(1, "2026-10-10T00:00:00", -92.00, "ANTHROPIC PBC")}, // 100 USD × 0.92
		nil,
	)
	eur := rankBankLineMatches(
		testInvoice("factuur", 92, "EUR", "", ""), // no date, no name: the weakest exact match
		[]map[string]any{bankRow(2, "", -92.00, "")},
		nil,
	)
	if len(fx) != 1 || len(eur) != 1 {
		t.Fatalf("got %d FX and %d EUR candidates, want 1 each", len(fx), len(eur))
	}
	if math.Abs(fx[0].Score-(fxAmountScoreMax+dateScoreMax+nameScore)) > 1e-6 {
		t.Errorf("best FX score = %v, want %v", fx[0].Score, fxAmountScoreMax+dateScoreMax+nameScore)
	}
	if math.Abs(eur[0].Score-eurAmountScoreMax) > 1e-6 {
		t.Errorf("exact EUR score = %v, want %v", eur[0].Score, eurAmountScoreMax)
	}
	if eur[0].Score <= fx[0].Score {
		t.Errorf("exact EUR (%v) must outrank the best FX match (%v)", eur[0].Score, fx[0].Score)
	}
}

func TestBankLineCandidateLegacyJSON(t *testing.T) {
	m := BankLineCandidate{ID: 1, Datum: "d", Bedrag: -1, Omschrijving: "o", CurrencyConverted: true, InvoiceCurrency: "CHF", InvoiceAmount: 2, Score: 99}.legacyJSON()
	for _, k := range []string{"id", "datum", "bedrag", "omschrijving", "currencyConverted", "invoiceCurrency", "invoiceAmount"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
	if _, ok := m["score"]; ok {
		t.Error("matchedBankLine must not carry score")
	}
	if len(m) != 7 {
		t.Errorf("got %d keys, want 7: %v", len(m), m)
	}
}

func TestBankLineMatchable(t *testing.T) {
	tests := []struct {
		name string
		inv  *claude.InvoiceData
		want bool
	}{
		{"nil", nil, false},
		{"factuur", testInvoice("factuur", 1, "EUR", "", ""), true},
		{"bonnetje", testInvoice("bonnetje", 1, "EUR", "", ""), true},
		{"creditnota", testInvoice("creditnota", 1, "EUR", "", ""), true},
		{"overig", testInvoice("overig", 1, "EUR", "", ""), false},
		{"Overig with spaces", testInvoice(" Overig ", 1, "EUR", "", ""), false},
		{"zero", testInvoice("factuur", 0, "EUR", "", ""), false},
		{"negative", testInvoice("factuur", -1, "EUR", "", ""), false},
		{"NaN", testInvoice("factuur", math.NaN(), "EUR", "", ""), false},
		{"Inf", testInvoice("factuur", math.Inf(1), "EUR", "", ""), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := bankLineMatchable(tc.inv); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSupplierNameTokens(t *testing.T) {
	tests := []struct {
		name string
		want []string
	}{
		{"Anthropic, PBC", []string{"anthropic"}},
		{"Acme B.V.", []string{"acme"}},
		{"Acme BV", []string{"acme"}},
		{"BV", nil},
		{"", nil},
		{"De Vries IT", []string{"vries"}},
		{"Café Zürich GmbH", []string{"café", "zürich"}},
		{"Tesla Supercharger-Netherlands", []string{"tesla", "supercharger", "netherlands"}},
		{"Shop 2000 Ltd.", []string{"shop", "2000"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := supplierNameTokens(tc.name); !slices.Equal(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseISODate(t *testing.T) {
	tests := []struct {
		in     string
		wantOK bool
		want   string
	}{
		{"2026-10-12T15:43:35.67", true, "2026-10-12"},
		{"2026-10-12T00:00:00", true, "2026-10-12"},
		{"2026-10-12", true, "2026-10-12"},
		{"  2026-10-12 ", true, "2026-10-12"},
		{"", false, ""},
		{"2026", false, ""},
		{"garbage!!!", false, ""},
		{"2026-13-45", false, ""},
		{"24-10-2026", false, ""},
		{"ééééééééééé", false, ""}, // multibyte: slicing must not panic
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := parseISODate(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && got.Format("2006-01-02") != tc.want {
				t.Errorf("got %s, want %s", got.Format("2006-01-02"), tc.want)
			}
		})
	}
}

func TestDateScore(t *testing.T) {
	inv := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		days int
		want float64
	}{
		{0, dateScoreMax},
		{45, dateScoreMax / 2},
		{90, 0},
		{91, 0},
		{-7, dateScoreMax * (1 - 7.0/90)},
		{-8, 0},
		{365, 0},
	}
	for _, tc := range tests {
		got := dateScore(inv, inv.AddDate(0, 0, tc.days))
		if math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("days=%d: got %v, want %v", tc.days, got, tc.want)
		}
	}
}
