package eboekhouden

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// Characterisation against main (a524a1a): the typed APIError replaced the
// ad-hoc fmt.Errorf errors of apiGet/apiPost. Handlers pass err.Error() to the
// user (BatchProcess results, "Factuur ontvangen mislukt: …", "Bonnetje boeken
// mislukt: …", the archive endpoints) and CreateRelation routes on the
// "e-Boekhouden: " prefix, so the text of an envelope error must be exactly
// what main produced, and the session-expired detection must be unchanged.
//
// Intended differences, not pinned here: a bare non-2xx status no longer
// carries the response body (main: "request to <path> returned <n>: <body>",
// now "request to <path> returned <n> (<len> bytes)") and the path loses its
// query string. Only the common prefix is pinned.

// mainEnvelopeMessage is main's envelopeMessage, verbatim.
func mainEnvelopeMessage(env errorEnvelope) string {
	msg := env.Message
	if msg == "" {
		msg = env.Code
	}
	if msg == "" {
		msg = env.ErrorType
	}
	const maxLen = 200
	if len(msg) > maxLen {
		msg = msg[:maxLen]
	}
	return msg
}

// parityCall is one exported client method that goes through apiGet/apiPost.
type parityCall struct {
	name string
	path string // request path without query, as the fake sees it
	// query is true when the method sends a query string. main put it into
	// the bare-status error text; the APIError strips it on purpose (a
	// relation search carries the supplier name there).
	query bool
	call  func(c *Client) error
}

func parityCalls() []parityCall {
	payload := json.RawMessage(`{"voorbeeld":true}`)
	return []parityCall{
		{"CreateMutatie", "/v1/api/mutatie", false, func(c *Client) error { _, err := c.CreateMutatie(payload); return err }},
		{"LinkFileToMutation", "/v1/api/folderkoppel/koppel/item", false, func(c *Client) error { _, err := c.LinkFileToMutation(payload); return err }},
		{"CreateRelation", "/v1/api/relatie/model", false, func(c *Client) error { _, err := c.CreateRelation(payload); return err }},
		{"UploadArchiveFile", "/v1/api/folder/upload", false, func(c *Client) error { _, err := c.UploadArchiveFile(payload); return err }},
		{"CreateArchiveFolder", "/v1/api/folder/directory", false, func(c *Client) error { _, err := c.CreateArchiveFolder(payload); return err }},
		{"GetActiveLedgerAccounts", "/v1/api/grootboekrekening/selectlist/actieveGBR", false, func(c *Client) error { _, err := c.GetActiveLedgerAccounts(); return err }},
		{"SearchRelations", "/v1/api/relatie/search", true, func(c *Client) error { _, err := c.SearchRelations("Voorbeeld Leverancier"); return err }},
		{"GetImportGrid", "/v1/api/import/gridtable", true, func(c *Client) error { _, err := c.GetImportGrid(0, 2000); return err }},
		{"GetArchiveFolders", "/v1/api/folder/directory", false, func(c *Client) error { _, err := c.GetArchiveFolders(); return err }},
		{"GetArchiveFiles", "/v1/api/folder/42/filelist", true, func(c *Client) error { _, err := c.GetArchiveFiles(42); return err }},
		{"GetBTWInfo", "/v1/api/btwinfo/info", false, func(c *Client) error { _, err := c.GetBTWInfo(); return err }},
		{"GetLastMutatieData", "/v1/api/mutatie/getlastmutatiedata", false, func(c *Client) error { _, err := c.GetLastMutatieData(); return err }},
		{"GetLastRelation", "/v1/api/relatie/last", true, func(c *Client) error { _, err := c.GetLastRelation(); return err }},
	}
}

func TestEnvelopeErrorTextMatchesMain(t *testing.T) {
	envelopes := []struct {
		name string
		env  errorEnvelope
	}{
		{"message wins", errorEnvelope{ErrorType: "validation", Message: "Voorbeeldfout: de datum valt in een afgesloten periode", Code: "VAL_001"}},
		{"code when no message", errorEnvelope{ErrorType: "validation", Code: "MUT_042"}},
		{"errorType when nothing else", errorEnvelope{ErrorType: "business"}},
		{"long message capped at 200 bytes", errorEnvelope{ErrorType: "validation", Message: strings.Repeat("Voorbeeld ", 40)}},
		{"multi-byte message capped like main", errorEnvelope{ErrorType: "validation", Message: strings.Repeat("ë", 150)}},
	}
	// main checked the envelope before the status, so a 4xx/5xx carrying an
	// envelope reads the same as a 200 envelope.
	statuses := []int{200, 400, 500}

	for _, call := range parityCalls() {
		for _, e := range envelopes {
			for _, status := range statuses {
				t.Run(fmt.Sprintf("%s/%s/%d", call.name, e.name, status), func(t *testing.T) {
					body, _ := json.Marshal(e.env)
					var last recordedRequest
					c := newTestClient(t, jsonServer(t, status, string(body), &last))

					err := call.call(c)
					want := "e-Boekhouden: " + mainEnvelopeMessage(e.env)
					if err == nil || err.Error() != want {
						t.Fatalf("error = %v, want %q (main's text)", err, want)
					}
					if errors.Is(err, ErrSessionExpired) {
						t.Error("an envelope error must not be a session expiry")
					}
				})
			}
		}
	}
}

func TestSessionExpiryDetectionMatchesMain(t *testing.T) {
	bodies := []struct {
		name   string
		status int
		body   string
	}{
		{"401 with JSON", 401, `{"message":"Unauthorized"}`},
		{"401 with HTML", 401, `<html>login</html>`},
		{"security envelope", 200, `{"errorType":"security","message":"Niet ingelogd","code":"SECURITY_010"}`},
		{"SECURITY_010 code alone", 200, `{"code":"SECURITY_010"}`},
		{"Niet ingelogd text on a 500", 500, `Niet ingelogd`},
	}
	for _, call := range parityCalls() {
		for _, b := range bodies {
			t.Run(call.name+"/"+b.name, func(t *testing.T) {
				var last recordedRequest
				c := newTestClient(t, jsonServer(t, b.status, b.body, &last))
				if err := call.call(c); !errors.Is(err, ErrSessionExpired) {
					t.Fatalf("error = %v, want ErrSessionExpired as on main", err)
				}
			})
		}
	}
}

func TestBareStatusErrorKeepsMainPrefix(t *testing.T) {
	cases := []struct {
		status int
		body   string
	}{
		{500, `<html>Voorbeeld storing</html>`},
		{404, `Not Found`},
		{400, `{"title":"One or more validation errors occurred.","errors":{"voorbeeld":["ongeldig"]}}`},
	}
	for _, call := range parityCalls() {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("%s/%d", call.name, tc.status), func(t *testing.T) {
				var last recordedRequest
				c := newTestClient(t, jsonServer(t, tc.status, tc.body, &last))
				err := call.call(c)
				if err == nil {
					t.Fatal("expected an error")
				}
				// main: "request to <path[?query]> returned <status>: <body>".
				// Without a query the prefix up to the status is main's text;
				// with one, only the query is gone (intended).
				prefix := fmt.Sprintf("request to %s returned %d", call.path, tc.status)
				if !strings.HasPrefix(err.Error(), prefix) {
					t.Errorf("error = %q, want prefix %q", err.Error(), prefix)
				}
				if call.query && strings.Contains(err.Error(), "?") {
					t.Errorf("error = %q still carries the query string", err.Error())
				}
				if strings.Contains(err.Error(), "Voorbeeld") || strings.Contains(err.Error(), "ongeldig") {
					t.Errorf("error = %q embeds the response body", err.Error())
				}
				if strings.HasPrefix(err.Error(), "e-Boekhouden: ") {
					t.Error("a bare status must not look like an envelope (CreateRelation would answer 400 instead of 502)")
				}
				var api *APIError
				if !errors.As(err, &api) || api.Status != tc.status {
					t.Errorf("err = %#v, want *APIError with status %d", err, tc.status)
				}
			})
		}
	}
}

func TestSuccessBodyPassesThroughLikeMain(t *testing.T) {
	const body = `{"mutNr":5001,"mutId":17}`
	var last recordedRequest
	c := newTestClient(t, jsonServer(t, 200, body, &last))
	raw, err := c.CreateMutatie(json.RawMessage(`{"mutatie":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != body {
		t.Errorf("raw = %s, want the body verbatim", raw)
	}
	assertRequest(t, last, "POST", "/v1/api/mutatie", `{"mutatie":{}}`)

	// POST with 201 succeeded on main as well.
	c = newTestClient(t, jsonServer(t, 201, body, &last))
	if _, err := c.CreateMutatie(json.RawMessage(`{}`)); err != nil {
		t.Errorf("201 on POST: %v, want success as on main", err)
	}
}
