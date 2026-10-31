package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// A fictitious sales invoice in the WKA shape: € 12.000 excl + € 2.520 BTW
// = € 14.520, paid as excl − 10% (10.800) on the bank account and 10% of
// excl (1.200) plus the BTW (2.520) on the G-rekening.
const (
	wkaExcl    = 12000.0
	wkaBTW     = 2520.0
	wkaTotal   = 14520.0
	wkaMain    = 10800.0
	wkaTen     = 1200.0
	wkaFactuur = "VF-2026-0017"
)

var wkaTotals = eboekhouden.MutatieTotals{Exclusief: wkaExcl, BTW: wkaBTW}

// debiteurPost builds an open Debiteur post the way ParseOpenPostGrid does.
func debiteurPost(mutNr int, bedrijf, factuur, mutDatum string, bedrag, betaald float64) eboekhouden.OpenPost {
	return eboekhouden.OpenPost{
		MutNr:       mutNr,
		Bedrijf:     bedrijf,
		MutFactuur:  factuur,
		MutDatum:    mutDatum,
		SBedrag:     bedrag,
		SBetaald:    betaald,
		TeOntvangen: bedrag - betaald,
		RelatieID:   9000 + mutNr,
		MutSoort:    2,
	}
}

// wkaLines are the three bank lines of one WKA payment, with the invoice
// number in every description (the bank truncated it on the last one).
func wkaLines() []bankLine {
	return []bankLine{
		{ID: 1, Datum: "2026-10-20T00:00:00", Bedrag: wkaMain, Omschrijving: "OVERBOEKING Voorbeeld Bouw B.V. " + wkaFactuur, GrootboekId: 1020},
		{ID: 2, Datum: "2026-10-20T00:00:00", Bedrag: wkaTen, Omschrijving: "OVERBOEKING G-REK Voorbeeld Bouw " + wkaFactuur, GrootboekId: 1022},
		{ID: 3, Datum: "2026-10-21T00:00:00", Bedrag: wkaBTW, Omschrijving: "OVERBOEKING G-REK Voorbeeld Bouw VF-2026-001", GrootboekId: 1022},
	}
}

func TestMentionsInvoiceNumber(t *testing.T) {
	tests := []struct {
		omschrijving, factuur string
		want                  bool
	}{
		{"Betaling VF-2026-0017 oktober", "VF-2026-0017", true},
		{"betaling vf 2026 0017", "VF-2026-0017", true},        // separators differ
		{"OVERBOEKING Klant VB0042", "VB00420", true},          // one character truncated
		{"OVERBOEKING Klant FACTUURVB0042", "VB00420", true},   // at the end of a token
		{"OVERBOEKING Klant VB004", "VB00420", true},           // two characters truncated
		{"OVERBOEKING Klant VB00", "VB00420", false},           // too much truncated
		{"Betaling INV-0 en meer", "INV-0043", false},          // too much truncated
		{"Betaling INV-0042 oktober", "INV-0043", false},       // a similar number is not a hit
		{"Café Zonnig VF-2026-0017", "VF-2026-0017", true},     // accented letters are separators
		{"Betaling factuur 2026", "2026-0042", false},          // a bare year is not the number
		{"Betaling 20260042", "2026-0042", true},               // digits as a whole token
		{"NL00BANK0120260042 overboeking", "2026-0042", false}, // digits inside an IBAN do not count
		{"NL00BANK0120260042 overboeking", "20260042", false},  // nor as a suffix of one
		{"Betaling 42", "42", false},                           // too short to mean anything
		{"", "VF-2026-0017", false},
		{"Betaling VF-2026-0017", "", false},
	}
	for _, tt := range tests {
		if got := mentionsInvoiceNumber(tt.omschrijving, tt.factuur); got != tt.want {
			t.Errorf("mentionsInvoiceNumber(%q, %q) = %v, want %v", tt.omschrijving, tt.factuur, got, tt.want)
		}
	}
}

func TestParseMutatieTotals(t *testing.T) {
	raw := json.RawMessage(`{"mutatie":{"nr":701,"soort":2,"factuur":"VF-2026-0017"},"mutatieRegels":[
		{"index":0,"bedragExclusief":10000,"btw":2100,"bedragInclusief":12100},
		{"index":1,"bedragExclusief":2000,"btw":420,"bedragInclusief":2420}],"folderKoppelId":0}`)
	got, err := eboekhouden.ParseMutatieTotals(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != wkaTotals {
		t.Errorf("totals = %+v, want %+v", got, wkaTotals)
	}
	if _, err := eboekhouden.ParseMutatieTotals(json.RawMessage(`nope`)); err == nil {
		t.Error("expected an error for invalid JSON")
	}
}

func TestWKAAmounts(t *testing.T) {
	got := wkaAmounts(wkaTotals)
	want := []int64{cents(wkaMain), cents(wkaTen), cents(wkaBTW), cents(wkaTen + wkaBTW)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("wkaAmounts = %v, want %v", got, want)
	}
	if got := wkaAmounts(eboekhouden.MutatieTotals{}); got != nil {
		t.Errorf("wkaAmounts without totals = %v, want nil", got)
	}
}

func TestMatchOpenPostGroups(t *testing.T) {
	invoice := debiteurPost(701, "Voorbeeld Bouw B.V.", wkaFactuur, "2026-10-01T00:00:00", wkaTotal, 0)
	second := debiteurPost(702, "Voorbeeld Bouw B.V.", "VF-2026-0018", "2026-10-05T00:00:00", wkaTotal, 0)

	type want struct {
		state, category, evidence, factuur string
		lineIDs                            []int
		candidates                         int
	}
	tests := []struct {
		name    string
		lines   []bankLine
		posts   []eboekhouden.OpenPost
		singles map[int]openPostMatch
		models  map[int]eboekhouden.MutatieTotals
		want    []want
	}{
		{
			name:  "three parts on two accounts, every line names the invoice: complete and auto",
			lines: wkaLines(),
			posts: []eboekhouden.OpenPost{invoice},
			want:  []want{{paymentGroupStateComplete, "auto", paymentGroupEvidenceInvoice, wkaFactuur, []int{1, 2, 3}, 1}},
		},
		{
			name: "a line that only names the customer makes the group review",
			lines: []bankLine{
				{ID: 1, Datum: "2026-10-20", Bedrag: wkaMain, Omschrijving: "Voorbeeld Bouw " + wkaFactuur},
				{ID: 2, Datum: "2026-10-20", Bedrag: wkaTen + wkaBTW, Omschrijving: "Voorbeeld Bouw G-rekening"},
			},
			posts: []eboekhouden.OpenPost{invoice},
			want:  []want{{paymentGroupStateComplete, "review", paymentGroupEvidenceName, wkaFactuur, []int{1, 2}, 1}},
		},
		{
			name: "two invoices of the same customer with the same amount: ambiguous, nothing picked",
			lines: []bankLine{
				{ID: 1, Datum: "2026-10-20", Bedrag: wkaMain, Omschrijving: "Voorbeeld Bouw betaling"},
				{ID: 2, Datum: "2026-10-20", Bedrag: wkaTen + wkaBTW, Omschrijving: "Voorbeeld Bouw G-rekening"},
			},
			posts: []eboekhouden.OpenPost{invoice, second},
			want:  []want{{paymentGroupStateAmbiguous, "review", paymentGroupEvidenceName, "", []int{1, 2}, 2}},
		},
		{
			name: "an amount coincidence without invoice number or name is no group",
			lines: []bankLine{
				{ID: 1, Datum: "2026-10-20", Bedrag: wkaMain, Omschrijving: "SEPA overboeking"},
				{ID: 2, Datum: "2026-10-20", Bedrag: wkaTen, Omschrijving: "SEPA overboeking"},
				{ID: 3, Datum: "2026-10-21", Bedrag: wkaBTW, Omschrijving: "SEPA overboeking"},
			},
			posts: []eboekhouden.OpenPost{invoice},
		},
		{
			name: "lines that pay more than is outstanding are no group",
			lines: []bankLine{
				{ID: 1, Datum: "2026-10-20", Bedrag: wkaMain, Omschrijving: "Voorbeeld Bouw " + wkaFactuur},
				{ID: 2, Datum: "2026-10-20", Bedrag: wkaTen + wkaBTW + 100, Omschrijving: "Voorbeeld Bouw " + wkaFactuur},
			},
			posts: []eboekhouden.OpenPost{invoice},
		},
		{
			name: "one line naming the invoice is a partial group, always review",
			lines: []bankLine{
				{ID: 1, Datum: "2026-10-20", Bedrag: wkaMain, Omschrijving: "Voorbeeld Bouw " + wkaFactuur},
			},
			posts: []eboekhouden.OpenPost{invoice},
			want:  []want{{paymentGroupStatePartial, "review", paymentGroupEvidenceInvoice, wkaFactuur, []int{1}, 1}},
		},
		{
			name: "name hit plus the WKA amount excl minus 10% is a partial group",
			lines: []bankLine{
				{ID: 1, Datum: "2026-10-20", Bedrag: wkaMain, Omschrijving: "OVERBOEKING Voorbeeld Bouw B.V."},
			},
			posts:  []eboekhouden.OpenPost{invoice},
			models: map[int]eboekhouden.MutatieTotals{701: wkaTotals},
			want:   []want{{paymentGroupStatePartial, "review", paymentGroupEvidenceName, wkaFactuur, []int{1}, 1}},
		},
		{
			name: "name-only lines whose sum is a WKA amount form a partial group together",
			lines: []bankLine{
				{ID: 1, Datum: "2026-10-20", Bedrag: 700, Omschrijving: "Voorbeeld Bouw G-rekening"},
				{ID: 2, Datum: "2026-10-20", Bedrag: 500, Omschrijving: "Voorbeeld Bouw G-rekening"},
			},
			posts:  []eboekhouden.OpenPost{invoice},
			models: map[int]eboekhouden.MutatieTotals{701: wkaTotals},
			want:   []want{{paymentGroupStatePartial, "review", paymentGroupEvidenceName, wkaFactuur, []int{1, 2}, 1}},
		},
		{
			name: "name hit with an arbitrary amount is no partial group",
			lines: []bankLine{
				{ID: 1, Datum: "2026-10-20", Bedrag: 999, Omschrijving: "OVERBOEKING Voorbeeld Bouw B.V."},
			},
			posts:  []eboekhouden.OpenPost{invoice},
			models: map[int]eboekhouden.MutatieTotals{701: wkaTotals},
		},
		{
			name: "without the invoice model a name hit alone is no partial group",
			lines: []bankLine{
				{ID: 1, Datum: "2026-10-20", Bedrag: wkaMain, Omschrijving: "OVERBOEKING Voorbeeld Bouw B.V."},
			},
			posts: []eboekhouden.OpenPost{invoice},
		},
		{
			name: "the remainder after an earlier partial payment is matched against what is still open",
			lines: []bankLine{
				{ID: 1, Datum: "2026-11-02", Bedrag: wkaTen, Omschrijving: "Voorbeeld Bouw G-rekening"},
				{ID: 2, Datum: "2026-11-02", Bedrag: wkaBTW, Omschrijving: "Voorbeeld Bouw G-rekening btw"},
			},
			posts: []eboekhouden.OpenPost{debiteurPost(701, "Voorbeeld Bouw B.V.", wkaFactuur, "2026-10-01", wkaTotal, wkaMain)},
			want:  []want{{paymentGroupStateComplete, "review", paymentGroupEvidenceName, wkaFactuur, []int{1, 2}, 1}},
		},
		{
			name:  "a line dated before the invoice or more than 120 days after it is no candidate",
			lines: append(wkaLines(), bankLine{ID: 4, Datum: "2026-09-30", Bedrag: 1, Omschrijving: "Voorbeeld Bouw " + wkaFactuur}, bankLine{ID: 5, Datum: "2027-03-01", Bedrag: 1, Omschrijving: "Voorbeeld Bouw " + wkaFactuur}),
			posts: []eboekhouden.OpenPost{invoice},
			want:  []want{{paymentGroupStateComplete, "auto", paymentGroupEvidenceInvoice, wkaFactuur, []int{1, 2, 3}, 1}},
		},
		{
			name:  "a post settled by a name-hit single match is left alone",
			lines: wkaLines(),
			posts: []eboekhouden.OpenPost{invoice},
			singles: map[int]openPostMatch{
				42: {Post: invoice, NameHit: true},
			},
		},
		{
			name:  "a line claimed by a name-hit single match is no candidate",
			lines: append(wkaLines(), bankLine{ID: 9, Datum: "2026-10-22", Bedrag: wkaTotal, Omschrijving: "Voorbeeld Bouw VF-2026-0018"}),
			posts: []eboekhouden.OpenPost{invoice, second},
			singles: map[int]openPostMatch{
				9: {Post: second, NameHit: true},
			},
			want: []want{{paymentGroupStateComplete, "auto", paymentGroupEvidenceInvoice, wkaFactuur, []int{1, 2, 3}, 1}},
		},
		{
			name:  "outgoing lines never form a group",
			lines: []bankLine{{ID: 1, Datum: "2026-10-20", Bedrag: -wkaMain, Omschrijving: "Voorbeeld Bouw " + wkaFactuur}, {ID: 2, Datum: "2026-10-20", Bedrag: -(wkaTen + wkaBTW), Omschrijving: "Voorbeeld Bouw " + wkaFactuur}},
			posts: []eboekhouden.OpenPost{invoice},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchOpenPostGroups(tt.lines, tt.posts, tt.singles, tt.models)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d groups %+v, want %d", len(got), got, len(tt.want))
			}
			for i, w := range tt.want {
				g := got[i]
				if g.State != w.state || g.Category != w.category || g.Evidence != w.evidence || g.Factuurnummer != w.factuur ||
					!reflect.DeepEqual(g.LineIDs, w.lineIDs) || len(g.Candidates) != w.candidates {
					t.Errorf("group %d = %+v, want %+v", i, g, w)
				}
				if g.ID != paymentGroupID(w.lineIDs) {
					t.Errorf("group id = %q, want the hash of %v", g.ID, w.lineIDs)
				}
				var sum float64
				for _, id := range g.LineIDs {
					for _, l := range tt.lines {
						if l.ID == id {
							sum += l.Bedrag
						}
					}
				}
				if g.Total != roundCents(sum) {
					t.Errorf("total = %v, want %v", g.Total, sum)
				}
			}
		})
	}
}

func TestMatchOpenPostGroupsIsDeterministic(t *testing.T) {
	invoice := debiteurPost(701, "Voorbeeld Bouw B.V.", wkaFactuur, "2026-10-01", wkaTotal, 0)
	other := debiteurPost(702, "Andere Klant", "AK-77", "2026-10-03", 300, 0)
	lines := append(wkaLines(),
		bankLine{ID: 4, Datum: "2026-10-22", Bedrag: 100, Omschrijving: "Andere Klant AK-77"},
		bankLine{ID: 5, Datum: "2026-10-23", Bedrag: 200, Omschrijving: "Andere Klant AK-77"},
	)
	forward := matchOpenPostGroups(lines, []eboekhouden.OpenPost{invoice, other}, nil, nil)

	reversedLines := make([]bankLine, len(lines))
	for i, l := range lines {
		reversedLines[len(lines)-1-i] = l
	}
	backward := matchOpenPostGroups(reversedLines, []eboekhouden.OpenPost{other, invoice}, nil, nil)
	if !reflect.DeepEqual(forward, backward) {
		t.Errorf("order changed the result:\n forward  %+v\n backward %+v", forward, backward)
	}
	if len(forward) != 2 || !reflect.DeepEqual(forward[0].LineIDs, []int{1, 2, 3}) || !reflect.DeepEqual(forward[1].LineIDs, []int{4, 5}) {
		t.Errorf("groups = %+v", forward)
	}
}

func TestPaymentGroupIDIsStable(t *testing.T) {
	if paymentGroupID([]int{3, 1, 2}) != paymentGroupID([]int{1, 2, 3}) {
		t.Error("id depends on order")
	}
	if paymentGroupID([]int{1, 2}) == paymentGroupID([]int{1, 3}) {
		t.Error("different lines share an id")
	}
	if id := paymentGroupID([]int{1, 2, 3}); !strings.HasPrefix(id, "pg-") || len(id) != 19 {
		t.Errorf("id = %q", id)
	}
}

func TestWithoutGroupedLines(t *testing.T) {
	invoice := debiteurPost(701, "Voorbeeld Bouw B.V.", wkaFactuur, "2026-10-01", wkaTotal, 0)
	other := debiteurPost(702, "Andere Klant", "AK-77", "2026-10-03", 300, 0)
	groups := matchOpenPostGroups(wkaLines(), []eboekhouden.OpenPost{invoice}, nil, nil)
	singles := map[int]openPostMatch{
		1: {Post: other, NameHit: false},   // amount-only single on a grouped line: dropped
		7: {Post: invoice, NameHit: false}, // amount-only single on the post the group pays: dropped
		8: {Post: other, NameHit: false},   // untouched
		9: {Post: invoice, NameHit: true},  // a name hit is never dropped
	}
	got := withoutGroupedLines(singles, groups)
	if _, ok := got[1]; ok {
		t.Error("single of a grouped line kept")
	}
	if _, ok := got[7]; ok {
		t.Error("amount-only single on the group's post kept")
	}
	if _, ok := got[8]; !ok {
		t.Error("unrelated single dropped")
	}
	if _, ok := got[9]; !ok {
		t.Error("name-hit single dropped")
	}
	if len(singles) != 4 {
		t.Error("input modified")
	}
}

func TestPaymentGroupModelTargets(t *testing.T) {
	complete := debiteurPost(701, "Voorbeeld Bouw B.V.", wkaFactuur, "2026-10-01", wkaTotal, 0)
	nameOnly := debiteurPost(702, "Andere Klant", "AK-77", "2026-10-03", 300, 0)
	invoiceOnly := debiteurPost(703, "Derde Partij", "DK-1234", "2026-10-04", 300, 0)
	newer := debiteurPost(704, "Andere Klant", "AK-78", "2026-10-09", 300, 0)
	lines := append(wkaLines(),
		bankLine{ID: 4, Datum: "2026-10-22", Bedrag: 100, Omschrijving: "Andere Klant"},
		bankLine{ID: 5, Datum: "2026-10-23", Bedrag: 100, Omschrijving: "Derde Partij DK-1234"},
	)
	got := paymentGroupModelTargets(lines, []eboekhouden.OpenPost{complete, nameOnly, invoiceOnly, newer}, nil)
	// The complete post needs no model, the invoice-only post neither; the
	// two name-only posts do, newest first.
	if want := []int{704, 702}; !reflect.DeepEqual(got, want) {
		t.Errorf("targets = %v, want %v", got, want)
	}
}

func TestApplyPaymentGroups(t *testing.T) {
	invoice := debiteurPost(701, "Voorbeeld Bouw B.V.", wkaFactuur, "2026-10-01", wkaTotal, 0)
	groups := matchOpenPostGroups(wkaLines(), []eboekhouden.OpenPost{invoice}, nil, nil)
	base := []inboxClassifiedRow{
		{ID: 1, Bedrag: wkaMain, Omschrijving: "a", Category: "manual", DocumentMatch: &inboxDocumentMatch{FileID: 5}},
		{ID: 2, Bedrag: wkaTen, Omschrijving: "b", Category: "review", Grootboekcode: "8000"},
		{ID: 3, Bedrag: wkaBTW, Omschrijving: "c", Category: "review", Learned: true},
		{ID: 4, Bedrag: 10, Omschrijving: "d", Category: "manual"},
	}
	rows := applyPaymentGroups(base, groups)
	for _, r := range rows[:3] {
		if r.PaymentGroupID != groups[0].ID || r.Category != "auto" || r.Soort != soortNameFactuurbetalingOntvangen ||
			r.RelatieID != invoice.RelatieID || r.Factuurnummer != wkaFactuur || r.Grootboekcode != "" || r.BTWCode != "GEEN" ||
			r.Learned || r.DocumentMatch != nil || r.OpenPostMatch || r.AIOmschrijving != r.Omschrijving {
			t.Errorf("row %d = %+v", r.ID, r)
		}
		if r.Indicator != "Voorbeeld Bouw B.V. · "+wkaFactuur+" · betaling in delen" {
			t.Errorf("indicator = %q", r.Indicator)
		}
	}
	if rows[3].PaymentGroupID != "" || rows[3].Category != "manual" {
		t.Errorf("ungrouped row changed: %+v", rows[3])
	}
	if base[0].PaymentGroupID != "" || base[0].DocumentMatch == nil {
		t.Error("base modified")
	}
	if got := groupedAutoLines(groups); !reflect.DeepEqual(got, map[int]bool{1: true, 2: true, 3: true}) {
		t.Errorf("groupedAutoLines = %v", got)
	}
}

func TestPaymentGroupIndicator(t *testing.T) {
	tests := []struct {
		g    paymentGroup
		want string
	}{
		{paymentGroup{State: paymentGroupStateComplete, paymentGroupPost: paymentGroupPost{RelatieNaam: "Acme", Factuurnummer: "F-1"}}, "Acme · F-1 · betaling in delen"},
		{paymentGroup{State: paymentGroupStatePartial, paymentGroupPost: paymentGroupPost{RelatieNaam: "Acme", Factuurnummer: "F-1"}}, "Acme · F-1 · deelbetaling"},
		{paymentGroup{State: paymentGroupStatePartial, paymentGroupPost: paymentGroupPost{Factuurnummer: "F-1"}}, "Onbekende relatie · F-1 · deelbetaling"},
		{paymentGroup{State: paymentGroupStateAmbiguous, Candidates: []paymentGroupCandidate{{paymentGroupPost: paymentGroupPost{RelatieNaam: "Acme"}}, {paymentGroupPost: paymentGroupPost{RelatieNaam: "Acme"}}}}, "Past bij 2 openstaande facturen van Acme"},
		{paymentGroup{State: paymentGroupStateAmbiguous, Candidates: []paymentGroupCandidate{{paymentGroupPost: paymentGroupPost{RelatieNaam: "Acme"}}, {paymentGroupPost: paymentGroupPost{RelatieNaam: "Globex"}}}}, "Past bij 2 openstaande facturen"},
	}
	for _, tt := range tests {
		if got := paymentGroupIndicator(tt.g); got != tt.want {
			t.Errorf("indicator = %q, want %q", got, tt.want)
		}
	}
}

func TestPaymentGroupJSONShape(t *testing.T) {
	invoice := debiteurPost(701, "Voorbeeld Bouw B.V.", wkaFactuur, "2026-10-01", wkaTotal, 0)
	groups := matchOpenPostGroups(wkaLines(), []eboekhouden.OpenPost{invoice}, nil, nil)
	b, err := json.Marshal(groups[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "state", "category", "relatieId", "relatieNaam", "factuurnummer", "factuurdatum", "sBedrag", "sBetaald", "teOntvangen", "lineIds", "total", "candidates", "evidence"} {
		if !containsJSONKey(b, key) {
			t.Errorf("%s missing in %s", key, b)
		}
	}
	var decoded struct {
		Candidates []struct {
			LineIDs []int `json:"lineIds"`
		} `json:"candidates"`
		Total float64 `json:"total"`
	}
	json.Unmarshal(b, &decoded)
	if len(decoded.Candidates) != 1 || !reflect.DeepEqual(decoded.Candidates[0].LineIDs, []int{1, 2, 3}) || decoded.Total != wkaTotal {
		t.Errorf("decoded %+v from %s", decoded, b)
	}
}

func TestFactuurbetalingRefusals(t *testing.T) {
	invoice := debiteurPost(701, "Voorbeeld Bouw B.V.", wkaFactuur, "2026-10-01", wkaTotal, 0)
	purchase := eboekhouden.OpenPost{MutNr: 801, RelatieID: 4711, Bedrijf: "Leverancier", MutFactuur: "INV-0042", TeOntvangen: 121}
	posts := openPosts{Debiteur: []eboekhouden.OpenPost{invoice}, Crediteur: []eboekhouden.OpenPost{purchase}}
	item := func(id, soort int, bedrag float64, relatie int, factuur string) inboxBatchItem {
		return inboxBatchItem{ID: id, Soort: soort, Bedrag: bedrag, RelatieId: relatie, Factuurnummer: factuur}
	}

	t.Run("a group that fits is accepted, one cent over too", func(t *testing.T) {
		items := []inboxBatchItem{
			item(1, 3, wkaMain, invoice.RelatieID, wkaFactuur),
			item(2, 3, wkaTen, invoice.RelatieID, " vf-2026-0017 "), // case and spaces do not matter
			item(3, 3, wkaBTW+0.01, invoice.RelatieID, wkaFactuur),
			item(4, 4, 121, 4711, "INV-0042"),
			item(5, 6, 50, 0, ""), // not a factuurbetaling
			item(6, 3, 10, 0, ""), // incomplete: left to the payload builder
		}
		if got := factuurbetalingRefusals(items, posts, nil); len(got) != 0 {
			t.Errorf("refusals = %v, want none", got)
		}
	})
	t.Run("a group two cents over the outstanding amount is refused as a whole", func(t *testing.T) {
		items := []inboxBatchItem{
			item(1, 3, wkaMain, invoice.RelatieID, wkaFactuur),
			item(2, 3, wkaTen, invoice.RelatieID, wkaFactuur),
			item(3, 3, wkaBTW+0.02, invoice.RelatieID, wkaFactuur),
			item(4, 4, 121, 4711, "INV-0042"),
		}
		got := factuurbetalingRefusals(items, posts, nil)
		want := "De factuur is in e-Boekhouden veranderd: er staat nu € 14.520,00 open. Controleer de betalingen en probeer het opnieuw."
		for _, id := range []int{1, 2, 3} {
			if got[id] != want {
				t.Errorf("refusal[%d] = %q, want %q", id, got[id], want)
			}
		}
		if _, ok := got[4]; ok {
			t.Error("the purchase payment was refused too")
		}
	})
	t.Run("a post that is no longer open is refused", func(t *testing.T) {
		items := []inboxBatchItem{item(1, 3, 100, invoice.RelatieID, "VF-2026-0099")}
		got := factuurbetalingRefusals(items, posts, nil)
		if !strings.Contains(got[1], "er staat nu € 0,00 open") {
			t.Errorf("refusal = %q", got[1])
		}
	})
	t.Run("a side whose grid is unavailable is refused rather than booked blind", func(t *testing.T) {
		items := []inboxBatchItem{item(1, 3, 100, invoice.RelatieID, wkaFactuur), item(2, 4, 121, 4711, "INV-0042")}
		got := factuurbetalingRefusals(items, posts, map[string]bool{eboekhouden.OpenPostDebiteur: true})
		if got[1] != msgOpenPostsUnavailable {
			t.Errorf("refusal[1] = %q", got[1])
		}
		if _, ok := got[2]; ok {
			t.Error("the crediteur item was refused although its grid was read")
		}
	})
}

// groupFake plays e-boekhouden for BatchProcess with an open-post grid: a
// bank account 1020 and a G-rekening 1022, one open sales invoice in the
// WKA shape and the three bank lines that pay it.
type groupFake struct {
	mu          sync.Mutex
	mutaties    []string
	gridStatus  int // 0 answers the grid
	teOntvangen float64
}

func (f *groupFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	buf.ReadFrom(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	write := func(v any) {
		b, _ := json.Marshal(v)
		w.Write(b)
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/api/openpost/gridtable/openpost":
		if f.gridStatus != 0 {
			w.WriteHeader(f.gridStatus)
			return
		}
		if r.URL.Query().Get("soort") != eboekhouden.OpenPostDebiteur {
			write(map[string]any{"colMetadata": []any{}, "data": [][]any{}})
			return
		}
		write(map[string]any{
			"colMetadata": []map[string]any{
				{"name": "mutDatum", "index": 0}, {"name": "sBedrag", "index": 1}, {"name": "mutFactuur", "index": 2},
				{"name": "mutNr", "index": 3}, {"name": "relatieId", "index": 4}, {"name": "bedrijf", "index": 5},
				{"name": "sBetaald", "index": 6}, {"name": "teOntvangen", "index": 7}, {"name": "mutSoort", "index": 8},
			},
			"data": [][]any{{"2026-10-01T00:00:00", wkaTotal, wkaFactuur, 701, 9701, "Voorbeeld Bouw B.V.", wkaTotal - f.teOntvangen, f.teOntvangen, 2}},
		})
	case r.Method == http.MethodGet && r.URL.Path == "/v1/api/grootboekrekening/selectlist/actieveGBR":
		write([]map[string]any{
			{"id": 1020, "code": "1020", "omschrijving": "Bank", "rekeningCategorie": "FIN"},
			{"id": 1022, "code": "1022", "omschrijving": "G-rekening", "rekeningCategorie": "FIN"},
		})
	case r.Method == http.MethodGet && r.URL.Path == "/v1/api/import/gridtable":
		write(map[string]any{"rowCount": 3, "data": [][]any{
			{1, "2026-10-20T00:00:00", "NL00VOOR0000000001", "2026-10-20T00:00:00", wkaMain, "OVERBOEKING Voorbeeld Bouw " + wkaFactuur, "", 1020, "", false, ""},
			{2, "2026-10-20T00:00:00", "NL00VOOR0000000002", "2026-10-20T00:00:00", wkaTen, "OVERBOEKING G-REK Voorbeeld Bouw " + wkaFactuur, "", 1022, "", false, ""},
			{3, "2026-10-21T00:00:00", "NL00VOOR0000000002", "2026-10-21T00:00:00", wkaBTW, "OVERBOEKING G-REK Voorbeeld Bouw VF-2026-001", "", 1022, "", false, ""},
		}})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/api/mutatie":
		f.mutaties = append(f.mutaties, buf.String())
		write(map[string]any{"mutNr": 760 + len(f.mutaties), "mutId": 1})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func groupBatchEnv(t *testing.T, fake *groupFake) *eboekhouden.Client {
	t.Helper()
	gin.SetMode(gin.TestMode)
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	client, err := eboekhouden.NewClientForTesting(srv.URL, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func wkaBatchItems() []inboxBatchItem {
	return []inboxBatchItem{
		{ID: 1, GrootboekId: 1020, Soort: 3, BTWCode: "GEEN", Omschrijving: "OVERBOEKING Voorbeeld Bouw " + wkaFactuur, Bedrag: wkaMain, RelatieId: 9701, Factuurnummer: wkaFactuur},
		{ID: 2, GrootboekId: 1022, Soort: 3, BTWCode: "GEEN", Omschrijving: "OVERBOEKING G-REK Voorbeeld Bouw " + wkaFactuur, Bedrag: wkaTen, RelatieId: 9701, Factuurnummer: wkaFactuur},
		{ID: 3, GrootboekId: 1022, Soort: 3, BTWCode: "GEEN", Omschrijving: "OVERBOEKING G-REK Voorbeeld Bouw VF-2026-001", Bedrag: wkaBTW, RelatieId: 9701, Factuurnummer: wkaFactuur},
	}
}

func callBatch(t *testing.T, client *eboekhouden.Client, items []inboxBatchItem) (int, map[string]any) {
	t.Helper()
	h := &InboxHandler{} // no session: no Redis or database is touched
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	b, _ := json.Marshal(map[string]any{"items": items})
	c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(session.ClientContextKey, client)
	h.BatchProcess(c)
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not JSON: %s", w.Body.String())
	}
	return w.Code, resp
}

func TestBatchProcessBooksAGroupInTheHARShape(t *testing.T) {
	fake := &groupFake{teOntvangen: wkaTotal}
	client := groupBatchEnv(t, fake)
	status, resp := callBatch(t, client, wkaBatchItems())
	if status != http.StatusOK {
		t.Fatalf("status %d: %v", status, resp)
	}
	results := resp["results"].([]any)
	if len(results) != 3 {
		t.Fatalf("results = %v", results)
	}
	for _, r := range results {
		if r.(map[string]any)["status"] != "ok" {
			t.Errorf("result %v", r)
		}
	}
	if len(fake.mutaties) != 3 {
		t.Fatalf("got %d mutaties, want 3", len(fake.mutaties))
	}
	wantRegels := []string{
		`{"bedrag":10800,"btw":0,"btwCode":"GEEN","factuur":"VF-2026-0017","index":0,"relatieId":9701}`,
		`{"bedrag":1200,"btw":0,"btwCode":"GEEN","factuur":"VF-2026-0017","index":0,"relatieId":9701}`,
		`{"bedrag":2520,"btw":0,"btwCode":"GEEN","factuur":"VF-2026-0017","index":0,"relatieId":9701}`,
	}
	wantRekening := []int{1020, 1022, 1022}
	wantDatum := []string{"2026-10-20", "2026-10-20", "2026-10-21"}
	for i, body := range fake.mutaties {
		var m struct {
			Mutatie struct {
				Rekening int     `json:"rekening"`
				Datum    string  `json:"datum"`
				Soort    int     `json:"soort"`
				InEx     *string `json:"inEx"`
			} `json:"mutatie"`
			MutatieRegels []json.RawMessage `json:"mutatieRegels"`
			ImportID      int               `json:"importId"`
		}
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			t.Fatal(err)
		}
		if m.Mutatie.Rekening != wantRekening[i] || m.Mutatie.Datum != wantDatum[i] || m.Mutatie.Soort != 3 || m.Mutatie.InEx != nil || m.ImportID != i+1 {
			t.Errorf("mutatie %d = %s", i, body)
		}
		if len(m.MutatieRegels) != 1 || string(m.MutatieRegels[0]) != wantRegels[i] {
			t.Errorf("regels %d = %s, want [%s]", i, m.MutatieRegels, wantRegels[i])
		}
		if strings.Contains(body, "tegenRekening") || strings.Contains(body, "bedragInclusief") {
			t.Errorf("mutatie %d carries fields a factuurbetaling must not: %s", i, body)
		}
	}
}

func TestBatchProcessRefusesAGroupOverTheOutstandingAmount(t *testing.T) {
	// The invoice was partly paid meanwhile: only the G-rekening part is open.
	fake := &groupFake{teOntvangen: wkaTen + wkaBTW}
	client := groupBatchEnv(t, fake)
	status, resp := callBatch(t, client, wkaBatchItems())
	if status != http.StatusOK {
		t.Fatalf("status %d: %v", status, resp)
	}
	if len(fake.mutaties) != 0 {
		t.Fatalf("%d mutaties were booked, want none", len(fake.mutaties))
	}
	results := resp["results"].([]any)
	if len(results) != 3 {
		t.Fatalf("results = %v", results)
	}
	want := "De factuur is in e-Boekhouden veranderd: er staat nu € 3.720,00 open. Controleer de betalingen en probeer het opnieuw."
	for _, r := range results {
		m := r.(map[string]any)
		if m["status"] != "error" || m["error"] != want {
			t.Errorf("result %v", m)
		}
	}
}

func TestBatchProcessAnswers412WhenTheGridSaysTheSessionExpired(t *testing.T) {
	fake := &groupFake{teOntvangen: wkaTotal, gridStatus: http.StatusUnauthorized}
	client := groupBatchEnv(t, fake)
	status, _ := callBatch(t, client, wkaBatchItems())
	if status != http.StatusPreconditionFailed {
		t.Errorf("status = %d, want 412", status)
	}
	if len(fake.mutaties) != 0 {
		t.Errorf("%d mutaties were booked, want none", len(fake.mutaties))
	}
}

func TestBatchProcessRefusesPaymentsWhenTheGridIsUnavailable(t *testing.T) {
	fake := &groupFake{teOntvangen: wkaTotal, gridStatus: http.StatusInternalServerError}
	client := groupBatchEnv(t, fake)
	items := append(wkaBatchItems(), inboxBatchItem{ID: 4, GrootboekId: 1020, Soort: 5, Grootboekcode: "1020", BTWCode: "GEEN", Omschrijving: "Rente", Bedrag: 1})
	status, resp := callBatch(t, client, items)
	if status != http.StatusOK {
		t.Fatalf("status %d: %v", status, resp)
	}
	results := resp["results"].([]any)
	for i, r := range results[:3] {
		if m := r.(map[string]any); m["status"] != "error" || m["error"] != msgOpenPostsUnavailable {
			t.Errorf("result %d = %v", i, m)
		}
	}
	if m := results[3].(map[string]any); m["status"] != "ok" {
		t.Errorf("the ordinary item was not booked: %v", m)
	}
	if len(fake.mutaties) != 1 {
		t.Errorf("got %d mutaties, want 1", len(fake.mutaties))
	}
}
