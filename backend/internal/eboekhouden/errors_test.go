package eboekhouden

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestAPIErrorShapes(t *testing.T) {
	env := newEnvelopeError(
		"/v1/api/relatie/search?query=Voorbeeld%20Leverancier",
		200,
		errorEnvelope{ErrorType: "validation", Message: "Relatie 'Voorbeeld Leverancier' bestaat al", Code: "VAL_007"},
		80,
	)
	if got := env.Error(); got != "e-Boekhouden: Relatie 'Voorbeeld Leverancier' bestaat al" {
		t.Errorf("Error() = %q", got)
	}
	logStr := env.LogString()
	for _, leak := range []string{"Voorbeeld", "query=", "bestaat"} {
		if strings.Contains(logStr, leak) {
			t.Errorf("LogString() leaks %q: %q", leak, logStr)
		}
	}
	for _, want := range []string{"path=/v1/api/relatie/search", "status=200", `code="VAL_007"`, "bodyLen=80"} {
		if !strings.Contains(logStr, want) {
			t.Errorf("LogString() = %q, want containing %q", logStr, want)
		}
	}

	st := newStatusError("/v1/api/mutatie?x=1", 500, 1234)
	if got := st.Error(); got != "request to /v1/api/mutatie returned 500 (1234 bytes)" {
		t.Errorf("Error() = %q", got)
	}

	// errors.As finds the APIError through wrapping, as handler.errClass relies on.
	wrapped := fmt.Errorf("booking: %w", st)
	var api *APIError
	if !errors.As(wrapped, &api) || api.Status != 500 {
		t.Errorf("errors.As(wrapped) = %v, want the 500 APIError", api)
	}
}

func TestAPIDoNeverEmbedsTheBody(t *testing.T) {
	const body = `{"secret":"Voorbeeld Leverancier 1234,56"}`
	var last recordedRequest
	srv := jsonServer(t, 500, body, &last)
	c := newTestClient(t, srv)

	_, err := c.apiGet("/v1/api/mutatie")
	if err == nil {
		t.Fatal("expected an error for a 500")
	}
	if strings.Contains(err.Error(), "Voorbeeld") {
		t.Errorf("error embeds the response body: %q", err)
	}
	var api *APIError
	if !errors.As(err, &api) {
		t.Fatalf("err = %T, want *APIError", err)
	}
	if api.Status != 500 || api.BodyLen != len(body) || api.Path != "/v1/api/mutatie" {
		t.Errorf("APIError = %+v", api)
	}
}

func TestAPIDoEnvelopeIsTypedAndCapped(t *testing.T) {
	long := strings.Repeat("x", 300)
	envelope := `{"errorType":"validation","message":"` + long + `","code":"VAL_001"}`
	var last recordedRequest
	srv := jsonServer(t, 200, envelope, &last)
	c := newTestClient(t, srv)

	_, err := c.apiPost("/v1/api/mutatie", []byte(`{}`))
	var api *APIError
	if !errors.As(err, &api) {
		t.Fatalf("err = %T (%v), want *APIError", err, err)
	}
	if api.Code != "VAL_001" || api.Status != 200 {
		t.Errorf("APIError = %+v", api)
	}
	if len(api.Message) != 200 {
		t.Errorf("message length = %d, want capped at 200", len(api.Message))
	}
	if !strings.HasPrefix(err.Error(), "e-Boekhouden: ") {
		t.Errorf("Error() = %q, want the e-Boekhouden prefix", err.Error())
	}
	if strings.Contains(api.LogString(), "xxx") {
		t.Errorf("LogString() contains the message: %q", api.LogString())
	}
}

func TestBinaryStatusErrorHasNoBody(t *testing.T) {
	var last recordedRequest
	srv := jsonServer(t, 502, `<html>Voorbeeld upstream page</html>`, &last)
	c := newTestClient(t, srv)
	_, _, _, err := c.apiGetBinary("/v1/api/folder/1/download", 1024)
	if err == nil || strings.Contains(err.Error(), "Voorbeeld") {
		t.Errorf("err = %v, want a status error without the body", err)
	}
}
