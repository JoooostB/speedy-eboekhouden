package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// relationModelJSON is the shape of GET /v1/api/relatie/{id}
// (eboekhouden-relatie-met-gb-edit.har), trimmed and with fictitious values.
const relationModelJSON = `{"id":54000001,"code":"VB01","bedrijf":"Voorbeeld Leverancier","adres":"Voorbeeldstraat 1",
	"isCrediteur":0,"bp":"B","iban":"","grootboekrekeningId":39000001,"kvk":""}`

func TestMatchedRelationFromSearch(t *testing.T) {
	got := matchedRelationFromSearch(json.RawMessage(`[
		{"id": 42, "code": "VB01", "bedrijf": "Voorbeeld Leverancier", "grootboekrekeningId": 12345},
		{"id": 43, "code": "VB02", "bedrijf": "Voorbeeld Leverancier 2"}
	]`))
	out, _ := json.Marshal(got)
	if want := `{"id":42,"code":"VB01","bedrijf":"Voorbeeld Leverancier","grootboekrekeningId":12345}`; string(out) != want {
		t.Errorf("got %s, want %s", out, want)
	}

	// A hit without the field leaves it unknown (null), so analyzeBytes
	// looks the relation up.
	got = matchedRelationFromSearch(json.RawMessage(`[{"id": 43, "code": "VB02", "bedrijf": "Voorbeeld Leverancier 2"}]`))
	if got == nil || got.GrootboekrekeningID != nil {
		t.Fatalf("want a relation with unknown ledger, got %+v", got)
	}
	out, _ = json.Marshal(got)
	if want := `{"id":43,"code":"VB02","bedrijf":"Voorbeeld Leverancier 2","grootboekrekeningId":null}`; string(out) != want {
		t.Errorf("got %s, want %s", out, want)
	}

	for _, raw := range []string{`[]`, `null`, `{"error":"x"}`, `not json`, `[{"code":"x"}]`} {
		if got := matchedRelationFromSearch(json.RawMessage(raw)); got != nil {
			t.Errorf("%s: want nil, got %+v", raw, *got)
		}
	}
}

func TestParseRelationModel(t *testing.T) {
	r, err := parseRelationModel(json.RawMessage(relationModelJSON))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(r)
	if want := `{"id":54000001,"code":"VB01","bedrijf":"Voorbeeld Leverancier","grootboekrekeningId":39000001}`; string(out) != want {
		t.Errorf("got %s, want %s", out, want)
	}

	r, err = parseRelationModel(json.RawMessage(`{"id":7,"code":"X","bedrijf":"Y"}`))
	if err != nil || r.GrootboekrekeningID == nil || *r.GrootboekrekeningID != 0 {
		t.Errorf("a model without the field means no default, got %+v, %v", r, err)
	}
}

func relationServer(t *testing.T, status int, body string) *eboekhouden.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/api/relatie/54000001" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	client, err := eboekhouden.NewClientForTesting(srv.URL, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestFillRelationDefaultLedger(t *testing.T) {
	r := &relationSummary{ID: 54000001}
	if err := fillRelationDefaultLedger(relationServer(t, http.StatusOK, relationModelJSON), r); err != nil {
		t.Fatal(err)
	}
	if r.GrootboekrekeningID == nil || *r.GrootboekrekeningID != 39000001 {
		t.Errorf("got %v, want 39000001", r.GrootboekrekeningID)
	}

	r = &relationSummary{ID: 54000001}
	err := fillRelationDefaultLedger(relationServer(t, http.StatusUnauthorized, ""), r)
	if !errors.Is(err, eboekhouden.ErrSessionExpired) {
		t.Errorf("want ErrSessionExpired, got %v", err)
	}
	if r.GrootboekrekeningID != nil {
		t.Errorf("a failed lookup must leave the field unknown, got %v", *r.GrootboekrekeningID)
	}
}

func TestGetRelation(t *testing.T) {
	run := func(client *eboekhouden.Client, id string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/relations/"+id, nil)
		c.Params = gin.Params{{Key: "id", Value: id}}
		c.Set(session.ContextKey, &session.SessionData{UserID: "user-1"})
		if client != nil {
			c.Set(session.ClientContextKey, client)
		}
		(&InvoiceHandler{}).GetRelation(c)
		return w
	}

	w := run(relationServer(t, http.StatusOK, relationModelJSON), "54000001")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	// Only the summary goes to the browser, never the address or contact details.
	if want := `{"id":54000001,"code":"VB01","bedrijf":"Voorbeeld Leverancier","grootboekrekeningId":39000001}`; w.Body.String() != want {
		t.Errorf("got %s, want %s", w.Body, want)
	}

	if w := run(relationServer(t, http.StatusUnauthorized, ""), "54000001"); w.Code != http.StatusPreconditionFailed {
		t.Errorf("expired session: status %d, want 412", w.Code)
	}
	if w := run(nil, "abc"); w.Code != http.StatusBadRequest {
		t.Errorf("bad id: status %d, want 400", w.Code)
	}
}
