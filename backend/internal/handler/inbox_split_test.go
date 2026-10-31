package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// splitFake plays e-boekhouden for a split bank line: one unprocessed
// restaurant line of -121 on bank account 1300 and the ledger accounts a
// split needs. All data is fictitious.
type splitFake struct {
	mutaties []string
}

func (f *splitFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
			{"id": 1300, "code": "1300", "omschrijving": "Bank", "rekeningCategorie": "BANK"},
			{"id": 4300, "code": "4300", "omschrijving": "Representatiekosten", "rekeningCategorie": "KOSTEN"},
			{"id": 4310, "code": "4310", "omschrijving": "Overige personeelskosten", "rekeningCategorie": "KOSTEN"},
		})
	case r.Method == http.MethodGet && r.URL.Path == "/v1/api/import/gridtable":
		write(map[string]any{"rowCount": 1, "data": [][]any{
			{77, "2026-10-13T00:00:00", "NL00VOOR0000000000", "2026-10-13T00:00:00", -121.0, "VOORBEELD RESTAURANT", "", 1300, "", false, ""},
		}})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/api/mutatie":
		var buf bytes.Buffer
		buf.ReadFrom(r.Body)
		f.mutaties = append(f.mutaties, buf.String())
		write(map[string]any{"mutNr": 900 + len(f.mutaties), "mutId": 1})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func runSplitBatch(t *testing.T, items []inboxBatchItem) (*splitFake, []string, map[int]map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	fake := &splitFake{}
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

	h := &InboxHandler{}
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
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("results = %s", w.Body.String())
	}
	byID := make(map[int]map[string]any)
	for _, r := range resp.Results {
		id, _ := r["id"].(float64)
		byID[int(id)] = r
	}
	return fake, learned, byID
}

func restaurantSplit() []regelInput {
	return []regelInput{
		{Omschrijving: "Drank", Grootboekcode: "4300", BTWCode: "HOOG_INK_21", BedragIncl: 24.20},
		{Omschrijving: "Eten", Grootboekcode: "4300", BTWCode: "LAAG_INK_9", BedragIncl: 86.80},
		{Omschrijving: "Fooi", Grootboekcode: "4310", BTWCode: "GEEN", BedragIncl: 10},
	}
}

func TestBatchProcessSplitsALineInTheInclShapeAndLearnsNothing(t *testing.T) {
	fake, learned, results := runSplitBatch(t, []inboxBatchItem{
		{ID: 77, GrootboekId: 1300, Soort: 6, Grootboekcode: "4300", BTWCode: "LAAG_INK_9", Omschrijving: "Voorbeeld Restaurant", Bedrag: 121, Regels: restaurantSplit()},
	})
	if results[77]["status"] != "ok" {
		t.Fatalf("result = %v", results[77])
	}
	if len(fake.mutaties) != 1 {
		t.Fatalf("got %d mutaties, want 1", len(fake.mutaties))
	}
	want := `{"importId":77,"mutatie":{"datum":"2026-10-13","inEx":"IN","omschrijving":"Voorbeeld Restaurant","rekening":1300,"soort":6},` +
		`"mutatieRegels":[{"bedrag":24.2,"bedragExclusief":20,"bedragInclusief":24.2,"btw":4.2,"btwCode":"HOOG_INK_21","index":0,"omschrijving":"Drank","tegenRekening":4300},` +
		`{"bedrag":86.8,"bedragExclusief":79.63,"bedragInclusief":86.8,"btw":7.17,"btwCode":"LAAG_INK_9","index":1,"omschrijving":"Eten","tegenRekening":4300},` +
		`{"bedrag":10,"bedragExclusief":10,"bedragInclusief":10,"btw":0,"btwCode":"GEEN","index":2,"omschrijving":"Fooi","tegenRekening":4310}]}`
	if fake.mutaties[0] != want {
		t.Errorf("mutatie =\n %s\nwant\n %s", fake.mutaties[0], want)
	}
	if len(learned) != 0 {
		t.Errorf("a split line must not be learned, got %v", learned)
	}
}

func TestBatchProcessRefusesASplitThatDoesNotMatchTheBankLine(t *testing.T) {
	regels := restaurantSplit()
	regels[2].BedragIncl = 8 // 119 against the grid's 121
	fake, learned, results := runSplitBatch(t, []inboxBatchItem{
		{ID: 77, GrootboekId: 1300, Soort: 6, Grootboekcode: "4300", BTWCode: "LAAG_INK_9", Omschrijving: "Voorbeeld Restaurant", Bedrag: 119, Regels: regels},
	})
	if results[77]["status"] != "error" || results[77]["error"] != "De regels tellen op tot € 119,00, het totaal is € 121,00." {
		t.Errorf("result = %v", results[77])
	}
	if len(fake.mutaties) != 0 || len(learned) != 0 {
		t.Errorf("nothing may be booked or learned, got %d mutaties, learned %v", len(fake.mutaties), learned)
	}
}

func TestBatchProcessRefusesSplitsOnUnknownLinesAndUnknownCodes(t *testing.T) {
	unknownCode := restaurantSplit()
	unknownCode[1].Grootboekcode = "9999"
	fake, _, results := runSplitBatch(t, []inboxBatchItem{
		{ID: 78, GrootboekId: 1300, Soort: 6, Grootboekcode: "4300", BTWCode: "GEEN", Omschrijving: "Weg", Bedrag: 121, Regels: restaurantSplit()},
		{ID: 77, GrootboekId: 1300, Soort: 6, Grootboekcode: "4300", BTWCode: "GEEN", Omschrijving: "Code", Bedrag: 121, Regels: unknownCode},
	})
	if results[78]["status"] != "error" || !strings.Contains(results[78]["error"].(string), "niet gevonden") {
		t.Errorf("result 78 = %v", results[78])
	}
	if results[77]["status"] != "error" || results[77]["error"] != "Regel 2: grootboekrekening 9999 bestaat niet in je administratie." {
		t.Errorf("result 77 = %v", results[77])
	}
	if len(fake.mutaties) != 0 {
		t.Errorf("nothing may be booked, got %d mutaties", len(fake.mutaties))
	}
}

func TestBatchProcessRefusesSplitOnAFactuurbetaling(t *testing.T) {
	// Refused before the open-post guard would look at it; the other item
	// of the batch still goes through.
	fake, _, results := runSplitBatch(t, []inboxBatchItem{
		{ID: 77, GrootboekId: 1300, Soort: 4, Grootboekcode: "", BTWCode: "GEEN", Omschrijving: "Betaling", Bedrag: 121, RelatieId: 55, Factuurnummer: "VB-1", Regels: restaurantSplit()},
	})
	if results[77]["status"] != "error" || results[77]["error"] != "Een factuurbetaling kan niet gesplitst worden." {
		t.Errorf("result = %v", results[77])
	}
	if len(fake.mutaties) != 0 {
		t.Errorf("nothing may be booked, got %d mutaties", len(fake.mutaties))
	}
}
