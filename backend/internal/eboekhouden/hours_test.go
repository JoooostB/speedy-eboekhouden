package eboekhouden

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A failed hour entry shows its error per row in the submit results, so
// e-boekhouden's own validation message has to survive (main showed the raw
// body); any other body is reduced to status and length and never kept.
func TestSubmitHourEntryErrors(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		body        string
		wantErr     string
		wantMessage string
	}{
		{"success", http.StatusOK, `{}`, "", ""},
		{"created", http.StatusCreated, `{}`, "", ""},
		{
			name:        "validation envelope keeps the message",
			status:      http.StatusBadRequest,
			body:        `{"errorType":"validation","message":"Datum ligt in een afgesloten periode","code":"VAL_001"}`,
			wantErr:     "e-Boekhouden: Datum ligt in een afgesloten periode",
			wantMessage: "Datum ligt in een afgesloten periode",
		},
		{
			name:    "other body is not echoed",
			status:  http.StatusInternalServerError,
			body:    `<html>Voorbeeld Klant interne fout</html>`,
			wantErr: "request to /v1/api/uur returned 500 (41 bytes)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/v1/api/uur" {
					t.Errorf("request %s %s, want POST /v1/api/uur", r.Method, r.URL.Path)
				}
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			c, err := NewClientForTesting(srv.URL, "test-token")
			if err != nil {
				t.Fatal(err)
			}

			err = c.SubmitHourEntry(HourEntry{})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err is %T, want *APIError", err)
			}
			if apiErr.Message != tt.wantMessage {
				t.Errorf("Message = %q, want %q", apiErr.Message, tt.wantMessage)
			}
			if strings.Contains(apiErr.LogString(), "afgesloten") || strings.Contains(apiErr.LogString(), "Voorbeeld") {
				t.Errorf("LogString leaks content: %s", apiErr.LogString())
			}
		})
	}
}
