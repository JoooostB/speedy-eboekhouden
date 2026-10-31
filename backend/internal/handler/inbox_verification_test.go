package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// A fictitious card check as a Dutch bank prints it: the charge and its
// refund share the reference P0000000001.
const (
	cardChargeDesc = "eCom, Betaalpas Voorbeeld Winkel P0000000001 NR:TERMBNET, 23.10.26/10:56 VOORBEELDWINKEL.COM, Land: IRL KAARTNUMMER: **1234"
	cardRefundDesc = "eCom, Betaalpas Voorbeeld Winkel P0000000001 NR:TERMBNET, 23.10.26/10:56 VOORBEELDWINKEL.COM, Land: IRL KAARTNUMMER: **1234"
	subscription   = "eCom, TERUGKEREND Voorbeeld Winkel P0000000002 Betaalpas NR:TERMBNET, 24.10.26/09:12 VOORBEELDWINKEL.COM, Land: IRL KAARTNUMMER: **1234"
)

func line(id int, datum string, bedrag float64, omschrijving string) bankLine {
	return bankLine{ID: id, Datum: datum + "T00:00:00", Bedrag: bedrag, Omschrijving: omschrijving, GrootboekId: 1020}
}

func TestDetectVerificationPayments(t *testing.T) {
	type want struct {
		kind     string
		pair     int
		conf     float64
		category string
	}
	tests := []struct {
		name  string
		lines []bankLine
		want  map[int]want
	}{
		{
			name:  "card check with its refund",
			lines: []bankLine{line(1, "2026-10-23", -1, cardChargeDesc), line(2, "2026-10-23", 1, cardRefundDesc)},
			want:  map[int]want{1: {"card_check", 2, 1, "auto"}, 2: {"card_check", 1, 1, "auto"}},
		},
		{
			name:  "refund two days later, two euros",
			lines: []bankLine{line(1, "2026-10-23", -2, cardChargeDesc), line(2, "2026-10-25", 2, cardRefundDesc)},
			want:  map[int]want{1: {"card_check", 2, 1, "auto"}, 2: {"card_check", 1, 1, "auto"}},
		},
		{
			name:  "single leg, charge",
			lines: []bankLine{line(1, "2026-10-23", -1, cardChargeDesc)},
			want:  map[int]want{1: {"card_check", 0, 1, "auto"}},
		},
		{
			name:  "single leg, refund",
			lines: []bankLine{line(2, "2026-10-23", 1, cardRefundDesc)},
			want:  map[int]want{2: {"card_check", 0, 1, "auto"}},
		},
		{
			name:  "pair five days apart is two singles",
			lines: []bankLine{line(1, "2026-10-20", -1, cardChargeDesc), line(2, "2026-10-25", 1, cardRefundDesc)},
			want:  map[int]want{1: {"card_check", 0, 1, "auto"}, 2: {"card_check", 0, 1, "auto"}},
		},
		{
			name:  "two euros without a partner is not a single leg",
			lines: []bankLine{line(1, "2026-10-23", -2, cardChargeDesc)},
			want:  map[int]want{},
		},
		{
			name:  "one cent IBAN check",
			lines: []bankLine{line(3, "2026-10-01", 0.01, "Naam check NL00VOOR0000000009 Voorbeeld Betaaldienst")},
			want:  map[int]want{3: {"iban_check", 0, 1, "auto"}},
		},
		{
			name:  "one cent outgoing is an IBAN check too",
			lines: []bankLine{line(3, "2026-10-01", -0.01, "Controle rekening")},
			want:  map[int]want{3: {"iban_check", 0, 1, "auto"}},
		},
		{
			name:  "25 cents is still auto",
			lines: []bankLine{line(3, "2026-10-01", 0.25, "Verificatie")},
			want:  map[int]want{3: {"iban_check", 0, 0.93, "auto"}},
		},
		{
			name:  "50 cents without card wording is an IBAN check to review",
			lines: []bankLine{line(3, "2026-10-01", 0.5, "Overboeking Voorbeeld Klant")},
			want:  map[int]want{3: {"iban_check", 0, 0.82, "review"}},
		},
		{
			name:  "99 cents is the floor",
			lines: []bankLine{line(3, "2026-10-01", -0.99, "Overboeking")},
			want:  map[int]want{3: {"iban_check", 0, 0.6, "review"}},
		},
		{
			name:  "one euro coffee without card wording or partner",
			lines: []bankLine{line(4, "2026-10-01", -1, "Koffie Voorbeeld Bar")},
			want:  map[int]want{},
		},
		{
			name:  "a recurring card subscription",
			lines: []bankLine{line(5, "2026-10-24", -43.38, subscription)},
			want:  map[int]want{},
		},
		{
			name:  "zero amount",
			lines: []bankLine{line(6, "2026-10-24", 0, cardChargeDesc)},
			want:  map[int]want{},
		},
		{
			name: "the subscription next to the pair does not join it",
			lines: []bankLine{
				line(1, "2026-10-23", -1, cardChargeDesc),
				line(5, "2026-10-24", -43.38, subscription),
				line(2, "2026-10-23", 1, cardRefundDesc),
			},
			want: map[int]want{1: {"card_check", 2, 1, "auto"}, 2: {"card_check", 1, 1, "auto"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectVerificationPayments(tt.lines)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d hits %v, want %d", len(got), got, len(tt.want))
			}
			for id, w := range tt.want {
				h, ok := got[id]
				if !ok {
					t.Errorf("line %d missing in %v", id, got)
					continue
				}
				if h.Kind != w.kind || h.PairLineID != w.pair || h.Confidence != w.conf || h.Category != w.category {
					t.Errorf("line %d = %+v, want %+v", id, h, w)
				}
			}
		})
	}
}

func TestCardReference(t *testing.T) {
	tests := []struct{ in, want string }{
		{cardChargeDesc, "P0000000001"},
		{"eCom, Betaalpas Voorbeeld Winkel NR:TERMBNET, 23.10.26/10:56", "NR:TERMBNET"},
		{"Betaalpas", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := cardReference(tt.in); got != tt.want {
			t.Errorf("cardReference(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFindBijzondereBaten(t *testing.T) {
	acc := func(id int, code, name, cat string) map[string]any {
		return map[string]any{"id": float64(id), "code": code, "omschrijving": name, "rekeningCategorie": cat}
	}
	tests := []struct {
		name string
		accs []map[string]any
		want ledgerAccount
		ok   bool
	}{
		{"by name, any code", []map[string]any{acc(1, "9300", "Overige opbrengsten", "VW"), acc(2, "9310", "Bijzondere  Baten", "VW")}, ledgerAccount{"9310", "Bijzondere  Baten"}, true},
		{"name wins over the 9300 fallback", []map[string]any{acc(1, "9300", "Overige baten", "VW"), acc(2, "8900", "bijzondere baten", "VW")}, ledgerAccount{"8900", "bijzondere baten"}, true},
		{"fallback 9300 keeps its own name", []map[string]any{acc(1, "1020", "Bank", "FIN"), acc(2, "9300", "Overige opbrengsten", "VW")}, ledgerAccount{"9300", "Overige opbrengsten"}, true},
		{"nothing", []map[string]any{acc(1, "1020", "Bank", "FIN")}, ledgerAccount{}, false},
		{"empty", nil, ledgerAccount{}, false},
	}
	for _, tt := range tests {
		got, ok := findBijzondereBaten(tt.accs)
		if got != tt.want || ok != tt.ok {
			t.Errorf("%s: findBijzondereBaten = (%+v, %v), want (%+v, %v)", tt.name, got, ok, tt.want, tt.ok)
		}
	}
}

func TestWithVerification(t *testing.T) {
	pair := verificationHit{Kind: verificationKindCard, PairLineID: 2, Confidence: 1, Category: "auto"}
	bb := ledgerAccount{Code: "9300", Name: "Bijzondere baten"}
	row := withVerification(inboxClassifiedRow{ID: 1, Bedrag: -1, Omschrijving: cardChargeDesc, Learned: true}, pair, bb)
	if row.Category != "auto" || row.Soort != "GeldUitgegeven" || row.BTWCode != "GEEN" || row.Grootboekcode != "9300" || row.Learned || row.NeedsInvoice {
		t.Errorf("pair row = %+v", row)
	}
	if row.Indicator != "Controlebetaling · boeken op 9300 Bijzondere baten" {
		t.Errorf("indicator = %q", row.Indicator)
	}
	if row.Verification == nil || row.Verification.Kind != "card_check" || row.Verification.PairLineID != 2 {
		t.Errorf("verification = %+v", row.Verification)
	}

	single := verificationHit{Kind: verificationKindCard, Confidence: 1, Category: "auto"}
	row = withVerification(inboxClassifiedRow{ID: 2, Bedrag: 1}, single, ledgerAccount{Code: "9300", Name: "Overige opbrengsten"})
	if row.Soort != "GeldOntvangen" || row.Indicator != "Controlebetaling zonder terugboeking in de inbox · boeken op 9300 Overige opbrengsten" {
		t.Errorf("single row = %+v", row)
	}

	row = withVerification(inboxClassifiedRow{ID: 3, Bedrag: 0.01}, verificationHit{Kind: verificationKindIBAN, Confidence: 1, Category: "auto"}, ledgerAccount{})
	if row.Category != "review" || row.Grootboekcode != "" || row.Indicator != "Controlebetaling. Geen rekening Bijzondere baten gevonden: kies zelf een grootboekrekening." {
		t.Errorf("row without ledger = %+v", row)
	}

	b, _ := json.Marshal(row)
	if !containsJSONKey(b, "verification") || strings.Contains(string(b), "pairLineId") {
		t.Errorf("json = %s", b)
	}
}

// verificationFake plays e-boekhouden for a BatchProcess of the card pair.
type verificationFake struct {
	mu       sync.Mutex
	mutaties []string
}

func (f *verificationFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		write(map[string]any{"colMetadata": []any{}, "data": [][]any{}})
	case r.Method == http.MethodGet && r.URL.Path == "/v1/api/grootboekrekening/selectlist/actieveGBR":
		write([]map[string]any{
			{"id": 1020, "code": "1020", "omschrijving": "Bank", "rekeningCategorie": "FIN"},
			{"id": 93001, "code": "9300", "omschrijving": "Bijzondere baten", "rekeningCategorie": "VW"},
		})
	case r.Method == http.MethodGet && r.URL.Path == "/v1/api/import/gridtable":
		write(map[string]any{"rowCount": 3, "data": [][]any{
			{1, "2026-10-23T00:00:00", "", "2026-10-23T00:00:00", -1.0, cardChargeDesc, "", 1020, "", false, ""},
			{2, "2026-10-23T00:00:00", "", "2026-10-23T00:00:00", 1.0, cardRefundDesc, "", 1020, "", false, ""},
			{3, "2026-10-24T00:00:00", "", "2026-10-24T00:00:00", -43.38, subscription, "", 1020, "", false, ""},
		}})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/api/mutatie":
		f.mutaties = append(f.mutaties, buf.String())
		write(map[string]any{"mutNr": 800 + len(f.mutaties), "mutId": 1})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestBatchProcessBooksAVerificationPairInTheHARShapeAndLearnsNothing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &verificationFake{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	client, err := eboekhouden.NewClientForTesting(srv.URL, "test-token")
	if err != nil {
		t.Fatal(err)
	}

	var learned []string
	orig := upsertLearned
	upsertLearned = func(_ *InboxHandler, _ context.Context, _ string, signal, grootboekcode, _, _, _ string) error {
		learned = append(learned, signal+"->"+grootboekcode)
		return nil
	}
	t.Cleanup(func() { upsertLearned = orig })

	items := []inboxBatchItem{
		{ID: 1, GrootboekId: 1020, Soort: 6, Grootboekcode: "9300", BTWCode: "GEEN", Omschrijving: cardChargeDesc, Bedrag: 1},
		{ID: 2, GrootboekId: 1020, Soort: 5, Grootboekcode: "9300", BTWCode: "GEEN", Omschrijving: cardRefundDesc, Bedrag: 1},
		{ID: 3, GrootboekId: 1020, Soort: 6, Grootboekcode: "4400", BTWCode: "GEEN", Omschrijving: subscription, Bedrag: 43.38},
	}
	h := &InboxHandler{} // no Redis, no database: the learn seam records instead
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	b, _ := json.Marshal(map[string]any{"items": items})
	c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(session.ClientContextKey, client)
	c.Set(session.ContextKey, &session.SessionData{ID: "s1", UserID: "u1"})
	h.BatchProcess(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Results []struct {
			ID     int    `json:"id"`
			Status string `json:"status"`
		} `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || len(resp.Results) != 3 {
		t.Fatalf("results = %s", w.Body.String())
	}
	for _, r := range resp.Results {
		if r.Status != "ok" {
			t.Errorf("result %+v", r)
		}
	}
	if len(fake.mutaties) != 3 {
		t.Fatalf("got %d mutaties, want 3", len(fake.mutaties))
	}

	// The HAR shape of the card check: soort 6, positive bedrag, GEEN, the
	// 9300 id as tegenRekening, importId at the top level. inEx stays "EX"
	// for soort 6 as every other Geld uitgegeven booking sends it.
	type regel struct {
		Index           int     `json:"index"`
		Bedrag          float64 `json:"bedrag"`
		BTW             float64 `json:"btw"`
		BTWCode         string  `json:"btwCode"`
		TegenRekening   int     `json:"tegenRekening"`
		BedragInclusief float64 `json:"bedragInclusief"`
		BedragExclusief float64 `json:"bedragExclusief"`
	}
	for i, wantSoort := range []int{6, 5} {
		var m struct {
			Mutatie struct {
				Rekening     int    `json:"rekening"`
				Datum        string `json:"datum"`
				Soort        int    `json:"soort"`
				InEx         string `json:"inEx"`
				Omschrijving string `json:"omschrijving"`
			} `json:"mutatie"`
			MutatieRegels []regel `json:"mutatieRegels"`
			ImportID      int     `json:"importId"`
		}
		if err := json.Unmarshal([]byte(fake.mutaties[i]), &m); err != nil {
			t.Fatal(err)
		}
		wantInEx := "EX"
		if wantSoort == 5 {
			wantInEx = "IN"
		}
		if m.Mutatie.Rekening != 1020 || m.Mutatie.Datum != "2026-10-23" || m.Mutatie.Soort != wantSoort || m.Mutatie.InEx != wantInEx || m.ImportID != i+1 {
			t.Errorf("mutatie %d = %s", i, fake.mutaties[i])
		}
		want := regel{Index: 0, Bedrag: 1, BTW: 0, BTWCode: "GEEN", TegenRekening: 93001, BedragInclusief: 1, BedragExclusief: 1}
		if len(m.MutatieRegels) != 1 || m.MutatieRegels[0] != want {
			t.Errorf("regels %d = %+v, want [%+v]", i, m.MutatieRegels, want)
		}
		if strings.Contains(fake.mutaties[i], "relatieId") || strings.Contains(fake.mutaties[i], "factuur") {
			t.Errorf("mutatie %d carries invoice fields: %s", i, fake.mutaties[i])
		}
	}

	// Only the subscription is learned; the two legs of the card check are
	// not, so the merchant's real charges are never auto-booked on 9300.
	if len(learned) != 1 || !strings.HasSuffix(learned[0], "->4400") {
		t.Errorf("learned = %v, want only the subscription on 4400", learned)
	}
}
