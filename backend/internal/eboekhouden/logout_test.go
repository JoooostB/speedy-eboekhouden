package eboekhouden

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withLogoutServer points logoutBaseURL at a server for the duration of the
// test and restores the production value afterwards.
func withLogoutServer(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prev := logoutBaseURL
	logoutBaseURL = srv.URL
	t.Cleanup(func() { logoutBaseURL = prev })
}

func TestLogoutBaseURLDefaultsToSecureHost(t *testing.T) {
	if logoutBaseURL != baseURLSecure {
		t.Errorf("logoutBaseURL = %q, want %q", logoutBaseURL, baseURLSecure)
	}
}

func TestLogout(t *testing.T) {
	const loginPage = `<html><body><form><input name="txtEmail"></form></body></html>`

	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"login page with 200", 200, loginPage, ""},
		{"redirect to the login page counts as done", 302, "", ""},
		{"server error", 500, "boom", "returned 500"},
		{"forbidden", 403, "", "returned 403"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var last recordedRequest
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				last = record(t, r)
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				if tc.status == 302 {
					w.Header().Set("Location", "/bh/inloggen.asp")
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			t.Cleanup(srv.Close)
			withLogoutServer(t, srv)
			c := newTestClient(t, srv)

			err := c.Logout()
			assertRequest(t, last, http.MethodGet, logoutPath, "")

			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestCheckAuth(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		wantValid bool
		wantErr   bool
	}{
		{"logged in", 200, `{"valid":true,"user":{"name":"Voorbeeld","email":"voorbeeld@example.com"}}`, true, false},
		{"logged out", 200, `{"valid":false,"currentDomain":0}`, false, false},
		{"401 is not valid, no error", 401, `Unauthorized`, false, false},
		{"security envelope is not valid, no error", 200, `{"errorType":"security","message":"Niet ingelogd","code":"SECURITY_010"}`, false, false},
		{"server error", 500, `boom`, false, true},
		{"garbage", 200, `not json`, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var last recordedRequest
			srv := jsonServer(t, tc.status, tc.body, &last)
			c := newTestClient(t, srv)

			valid, err := c.CheckAuth()
			// POST without a body: the request must carry the cookie and no payload.
			assertRequest(t, last, http.MethodPost, authCheckPath, "")

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if valid != tc.wantValid {
				t.Errorf("valid = %v, want %v", valid, tc.wantValid)
			}
		})
	}
}
