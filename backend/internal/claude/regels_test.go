package claude

import (
	"encoding/json"
	"strings"
	"testing"
)

// All data is fictitious ("Voorbeeld Cadeauwinkel").

func giftRegels() []InvoiceRegel {
	return []InvoiceRegel{
		{Omschrijving: "Verjaardagscadeau", BedragIncl: 35.80, BTWPercentage: 9, BTWCode: "LAAG_INK_9", Grootboekcode: "4500", Soort: "product"},
		{Omschrijving: "Verzendkosten", BedragIncl: 3.95, BTWPercentage: 9, BTWCode: "LAAG_INK_9", Grootboekcode: "4700", Soort: "verzending"},
	}
}

func TestNormalizeRegels(t *testing.T) {
	nine := make([]InvoiceRegel, 9)
	for i := range nine {
		nine[i] = InvoiceRegel{BedragIncl: 1, BTWCode: "GEEN", Grootboekcode: "4000", Soort: "overig"}
	}
	cases := []struct {
		name   string
		total  float64
		regels []InvoiceRegel
		advies *SplitsAdvies
		kept   bool
		reden  string
	}{
		{"adds up", 39.75, giftRegels(), &SplitsAdvies{Reden: "WKR", Toelichting: " Het cadeau valt onder de WKR, de verzendkosten niet. "}, true, SplitsRedenWKR},
		{"one cent off", 39.76, giftRegels(), &SplitsAdvies{Reden: "wkr"}, true, SplitsRedenWKR},
		{"two cents off", 39.77, giftRegels(), &SplitsAdvies{Reden: "wkr"}, false, ""},
		{"one regel", 35.80, giftRegels()[:1], &SplitsAdvies{Reden: "wkr"}, false, ""},
		{"nine regels", 9, nine, &SplitsAdvies{Reden: "overig"}, false, ""},
		{"negative regel", 31.85, append(giftRegels(), InvoiceRegel{BedragIncl: -7.90, BTWCode: "GEEN"}), nil, false, ""},
		{"no advice next to valid regels", 39.75, giftRegels(), nil, true, SplitsRedenOverig},
		{"unknown reden", 39.75, giftRegels(), &SplitsAdvies{Reden: "cadeau"}, true, SplitsRedenOverig},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &InvoiceData{BedragInclBTW: tc.total, Regels: tc.regels, SplitsAdvies: tc.advies}
			NormalizeRegels(d)
			if tc.kept {
				if len(d.Regels) != len(tc.regels) || d.SplitsAdvies == nil || d.SplitsAdvies.Reden != tc.reden {
					t.Fatalf("regels = %d, advies = %+v, want kept with reden %s", len(d.Regels), d.SplitsAdvies, tc.reden)
				}
				if d.SplitsAdvies.Toelichting != "" && d.SplitsAdvies.Toelichting != "Het cadeau valt onder de WKR, de verzendkosten niet." {
					t.Errorf("toelichting not trimmed: %q", d.SplitsAdvies.Toelichting)
				}
				return
			}
			if d.Regels != nil || d.SplitsAdvies != nil {
				t.Errorf("regels = %v, advies = %v, want dropped", d.Regels, d.SplitsAdvies)
			}
		})
	}
}

func TestNormalizeRegelsCleansEachRegel(t *testing.T) {
	d := &InvoiceData{BedragInclBTW: 10, Regels: []InvoiceRegel{
		{Omschrijving: " Eten ", BedragIncl: 8, BTWCode: " laag_ink_9 ", Grootboekcode: " 4300 ", Soort: " Product "},
		{Omschrijving: "Fooi", BedragIncl: 2, BTWCode: "GEEN", Grootboekcode: "4300", Soort: "tip"},
	}}
	NormalizeRegels(d)
	if d.Regels[0].Omschrijving != "Eten" || d.Regels[0].BTWCode != "LAAG_INK_9" || d.Regels[0].Grootboekcode != "4300" || d.Regels[0].Soort != "product" {
		t.Errorf("regel 1 = %+v", d.Regels[0])
	}
	if d.Regels[1].Soort != "overig" {
		t.Errorf("unknown soort = %q, want overig", d.Regels[1].Soort)
	}
	NormalizeRegels(nil) // must not panic
}

// TestInvoiceDataCacheEntryWithoutRegels replays an analysis cached by
// prompt version 2: no regels, no splitsAdvies. It must load, normalise to
// "no proposal" and marshal without the new keys.
func TestInvoiceDataCacheEntryWithoutRegels(t *testing.T) {
	legacy := `{"leverancier":"Voorbeeld Cadeauwinkel","factuurnummer":"2026-0042","bedragInclBtw":39.75,"btwCode":"LAAG_INK_9","documentType":"factuur","belastingAdvies":[]}`
	var d InvoiceData
	if err := json.Unmarshal([]byte(legacy), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	NormalizeDocumentType(&d)
	NormalizeRegels(&d)
	if d.Regels != nil || d.SplitsAdvies != nil {
		t.Errorf("regels = %v, advies = %v, want none", d.Regels, d.SplitsAdvies)
	}
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"regels", "splitsAdvies"} {
		if _, ok := m[key]; ok {
			t.Errorf("%s must be omitted without a proposal", key)
		}
	}
	if m["leverancier"] != "Voorbeeld Cadeauwinkel" || m["bedragInclBtw"] != 39.75 {
		t.Errorf("round trip lost fields: %v", m)
	}

	// And a version-3 entry round-trips with them.
	d.Regels = giftRegels()
	d.SplitsAdvies = &SplitsAdvies{Reden: SplitsRedenWKR, Toelichting: "Het cadeau valt onder de WKR, de verzendkosten niet."}
	out, _ = json.Marshal(d)
	var back InvoiceData
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Regels) != 2 || back.SplitsAdvies == nil || back.SplitsAdvies.Reden != SplitsRedenWKR {
		t.Errorf("round trip = %+v", back)
	}
}

func TestInvoicePromptAsksForRegelsOnlyWhenTaxRelevant(t *testing.T) {
	prompt := buildInvoicePrompt([]LedgerAccountInfo{{Code: "4500", Omschrijving: "Personeelsgeschenken"}})
	for _, want := range []string{"- regels:", "- splitsAdvies:", "werkkostenregeling", "at most 8 regels", `"fooi"`, "btw_tarieven"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
}
