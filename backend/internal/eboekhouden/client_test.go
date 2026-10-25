package eboekhouden

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testToken is the auth token every test client sends; the fake server checks
// that it arrives as the auth-token cookie.
const testToken = "test-auth-token"

// recordedRequest captures what matters about a request to the fake
// e-boekhouden server so a test can assert on it after the client call.
type recordedRequest struct {
	Method    string
	Path      string // path plus query string, as sent
	Body      string
	AuthToken string // value of the auth-token cookie, "" when absent
}

// record reads the request on the server side. It must not call t.Fatal:
// the handler runs on a different goroutine than the test.
func record(t *testing.T, r *http.Request) recordedRequest {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Errorf("reading request body: %v", err)
	}
	rec := recordedRequest{Method: r.Method, Path: r.URL.RequestURI(), Body: string(body)}
	if c, err := r.Cookie("auth-token"); err == nil {
		rec.AuthToken = c.Value
	}
	return rec
}

// newTestClient returns a client that talks to srv instead of e-boekhouden.
func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c, err := NewClientWithToken(testToken)
	if err != nil {
		t.Fatal(err)
	}
	c.setBaseURL(srv.URL)
	return c
}

// jsonServer answers every request with the given status and JSON body and
// records the most recent request into *last.
func jsonServer(t *testing.T, status int, body string, last *recordedRequest) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*last = record(t, r)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// assertRequest checks method, path, body and the auth cookie of a recorded
// request.
func assertRequest(t *testing.T, got recordedRequest, method, path, body string) {
	t.Helper()
	if got.Method != method {
		t.Errorf("method = %q, want %q", got.Method, method)
	}
	if got.Path != path {
		t.Errorf("path = %q, want %q", got.Path, path)
	}
	if got.Body != body {
		t.Errorf("body = %q, want %q", got.Body, body)
	}
	if got.AuthToken != testToken {
		t.Errorf("auth-token cookie = %q, want %q", got.AuthToken, testToken)
	}
}

func TestConstructorsUseProductionBaseURL(t *testing.T) {
	c, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	if c.baseURL != baseURLSecure20 {
		t.Errorf("NewClient baseURL = %q, want %q", c.baseURL, baseURLSecure20)
	}

	c, err = NewClientWithToken("tok")
	if err != nil {
		t.Fatal(err)
	}
	if c.baseURL != baseURLSecure20 {
		t.Errorf("NewClientWithToken baseURL = %q, want %q", c.baseURL, baseURLSecure20)
	}
	if c.GetAuthToken() != "tok" {
		t.Errorf("GetAuthToken() = %q, want %q", c.GetAuthToken(), "tok")
	}
}

// Every client is bounded, so a stalled e-boekhouden ends in an error
// instead of a request that hangs until the connection drops.
func TestConstructorsSetRequestTimeout(t *testing.T) {
	for name, build := range map[string]func() (*Client, error){
		"NewClient":          NewClient,
		"NewClientWithToken": func() (*Client, error) { return NewClientWithToken("tok") },
		"NewClientForTesting": func() (*Client, error) {
			return NewClientForTesting("http://127.0.0.1:1", "tok")
		},
	} {
		c, err := build()
		if err != nil {
			t.Fatal(err)
		}
		if c.httpClient.Timeout != requestTimeout || requestTimeout <= 0 {
			t.Errorf("%s: Timeout = %v, want %v", name, c.httpClient.Timeout, requestTimeout)
		}
	}
}

func TestAPIPutAndDelete(t *testing.T) {
	const envelope = `{"errorType":"validation","message":"Naam is verplicht","code":"VAL_001"}`
	const securityEnvelope = `{"errorType":"security","message":"Niet ingelogd","code":"SECURITY_010"}`

	put := func(c *Client) (json.RawMessage, error) {
		return c.apiPut("/v1/api/folder/filename", json.RawMessage(`{"folderId":1}`))
	}
	del := func(c *Client) (json.RawMessage, error) {
		return c.apiDelete("/v1/api/folder/5")
	}

	tests := []struct {
		name        string
		call        func(*Client) (json.RawMessage, error)
		status      int
		resp        string
		wantMethod  string
		wantPath    string
		wantBody    string
		wantResp    string
		wantErr     string // substring of the error; "" means success
		wantExpired bool
	}{
		{"put ok", put, 200, `true`, "PUT", "/v1/api/folder/filename", `{"folderId":1}`, `true`, "", false},
		{"put created", put, 201, `true`, "PUT", "/v1/api/folder/filename", `{"folderId":1}`, `true`, "", false},
		{"put envelope", put, 200, envelope, "PUT", "/v1/api/folder/filename", `{"folderId":1}`, "", "e-Boekhouden: Naam is verplicht", false},
		{"put 401", put, 401, `Unauthorized`, "PUT", "/v1/api/folder/filename", `{"folderId":1}`, "", "", true},
		{"put security envelope", put, 200, securityEnvelope, "PUT", "/v1/api/folder/filename", `{"folderId":1}`, "", "", true},
		{"put 500", put, 500, `boom`, "PUT", "/v1/api/folder/filename", `{"folderId":1}`, "", "returned 500", false},
		{"delete ok", del, 200, `true`, "DELETE", "/v1/api/folder/5", "", `true`, "", false},
		{"delete envelope", del, 200, envelope, "DELETE", "/v1/api/folder/5", "", "", "e-Boekhouden: Naam is verplicht", false},
		{"delete 401", del, 401, ``, "DELETE", "/v1/api/folder/5", "", "", "", true},
		{"delete security envelope", del, 200, securityEnvelope, "DELETE", "/v1/api/folder/5", "", "", "", true},
		{"delete 404", del, 404, `not found`, "DELETE", "/v1/api/folder/5", "", "", "returned 404", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var last recordedRequest
			srv := jsonServer(t, tc.status, tc.resp, &last)
			c := newTestClient(t, srv)

			got, err := tc.call(c)
			assertRequest(t, last, tc.wantMethod, tc.wantPath, tc.wantBody)

			switch {
			case tc.wantExpired:
				if !errors.Is(err, ErrSessionExpired) {
					t.Fatalf("err = %v, want ErrSessionExpired", err)
				}
			case tc.wantErr != "":
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if string(got) != tc.wantResp {
					t.Errorf("response = %q, want %q", got, tc.wantResp)
				}
			}
		})
	}
}

func TestAPIGetBinary(t *testing.T) {
	pdf := []byte("%PDF-1.4 fake invoice content")
	const path = "/v1/api/folder/42/download"

	tests := []struct {
		name        string
		status      int
		headers     map[string]string
		body        []byte
		maxBytes    int64
		wantType    string
		wantName    string
		wantErr     string
		wantExpired bool
	}{
		{
			name:     "plain filename token",
			status:   200,
			headers:  map[string]string{"Content-Type": "application/pdf", "Content-Disposition": "attachment; filename=factuur.pdf"},
			body:     pdf,
			maxBytes: 1024,
			wantType: "application/pdf",
			wantName: "factuur.pdf",
		},
		{
			name:     "utf-8 filename* wins over plain filename",
			status:   200,
			headers:  map[string]string{"Content-Type": "application/pdf", "Content-Disposition": "attachment; filename=Caf_.pdf; filename*=UTF-8''Caf%C3%A9%20bon.pdf"},
			body:     pdf,
			maxBytes: 1024,
			wantType: "application/pdf",
			wantName: "Café bon.pdf",
		},
		{
			name:     "quoted filename with spaces",
			status:   200,
			headers:  map[string]string{"Content-Type": "image/jpeg", "Content-Disposition": `attachment; filename="Bon oktober.jpg"`},
			body:     []byte{0xFF, 0xD8, 0xFF, 0xE0},
			maxBytes: 1024,
			wantType: "image/jpeg",
			wantName: "Bon oktober.jpg",
		},
		{
			name:     "no content-disposition",
			status:   200,
			headers:  map[string]string{"Content-Type": "application/pdf"},
			body:     pdf,
			maxBytes: 1024,
			wantType: "application/pdf",
			wantName: "",
		},
		{
			name:     "exactly max bytes is allowed",
			status:   200,
			headers:  map[string]string{"Content-Type": "application/pdf"},
			body:     pdf,
			maxBytes: int64(len(pdf)),
			wantType: "application/pdf",
		},
		{
			name:     "oversize",
			status:   200,
			headers:  map[string]string{"Content-Type": "application/pdf"},
			body:     pdf,
			maxBytes: int64(len(pdf)) - 1,
			wantErr:  "exceeds",
		},
		{
			name:        "401 is session expired",
			status:      401,
			headers:     map[string]string{"Content-Type": "text/plain"},
			body:        []byte("Unauthorized"),
			maxBytes:    1024,
			wantExpired: true,
		},
		{
			name:        "json security envelope with 200 is session expired",
			status:      200,
			headers:     map[string]string{"Content-Type": "application/json; charset=utf-8"},
			body:        []byte(`{"errorType":"security","message":"Niet ingelogd","code":"SECURITY_010"}`),
			maxBytes:    1024,
			wantExpired: true,
		},
		{
			name:     "json error envelope with 200",
			status:   200,
			headers:  map[string]string{"Content-Type": "application/json; charset=utf-8"},
			body:     []byte(`{"errorType":"validation","message":"Bestand niet gevonden","code":"FOLDER_404"}`),
			maxBytes: 1024,
			wantErr:  "e-Boekhouden: Bestand niet gevonden",
		},
		{
			name:     "500",
			status:   500,
			headers:  map[string]string{"Content-Type": "text/html"},
			body:     []byte("<html>boom</html>"),
			maxBytes: 1024,
			wantErr:  "returned 500",
		},
		{
			name:     "binary body containing an error marker is still a file",
			status:   200,
			headers:  map[string]string{"Content-Type": "application/pdf"},
			body:     []byte("%PDF-1.4 SECURITY_010 Niet ingelogd"),
			maxBytes: 1024,
			wantType: "application/pdf",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var last recordedRequest
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				last = record(t, r)
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				w.Write(tc.body)
			}))
			t.Cleanup(srv.Close)
			c := newTestClient(t, srv)

			data, contentType, filename, err := c.apiGetBinary(path, tc.maxBytes)
			assertRequest(t, last, "GET", path, "")

			switch {
			case tc.wantExpired:
				if !errors.Is(err, ErrSessionExpired) {
					t.Fatalf("err = %v, want ErrSessionExpired", err)
				}
			case tc.wantErr != "":
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				if data != nil {
					t.Errorf("data = %d bytes on error, want nil", len(data))
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !bytes.Equal(data, tc.body) {
					t.Errorf("data = %q, want %q", data, tc.body)
				}
				if contentType != tc.wantType {
					t.Errorf("contentType = %q, want %q", contentType, tc.wantType)
				}
				if filename != tc.wantName {
					t.Errorf("filename = %q, want %q", filename, tc.wantName)
				}
			}
		})
	}
}

func TestContentDispositionFilename(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"", ""},
		{"attachment", ""},
		{"attachment; filename=factuur.pdf", "factuur.pdf"},
		{`attachment; filename="Factuur oktober.pdf"`, "Factuur oktober.pdf"},
		// RFC 5987: filename* is decoded and takes precedence, regardless of order.
		{"attachment; filename=euro.pdf; filename*=UTF-8''%E2%82%AC.pdf", "€.pdf"},
		{"attachment; filename*=UTF-8''%E2%82%AC.pdf; filename=euro.pdf", "€.pdf"},
		{"attachment; filename*=utf-8''Caf%C3%A9.pdf", "Café.pdf"},
		// Directory parts from upstream are never trusted.
		{`attachment; filename="../../etc/passwd"`, "passwd"},
		{`attachment; filename="C:\\map\\bon.jpg"`, "bon.jpg"},
		// An unquoted value with spaces is malformed; we degrade to "" rather than fail the download.
		{"attachment; filename=My File.pdf", ""},
	}
	for _, tc := range tests {
		if got := contentDispositionFilename(tc.header); got != tc.want {
			t.Errorf("contentDispositionFilename(%q) = %q, want %q", tc.header, got, tc.want)
		}
	}
}

func TestCapMessage(t *testing.T) {
	long := strings.Repeat("x", 250)
	if got := capMessage(long); len(got) != 200 {
		t.Errorf("len(capMessage(250 chars)) = %d, want 200", len(got))
	}
	if got := capMessage("kort"); got != "kort" {
		t.Errorf("capMessage(short) = %q, want unchanged", got)
	}
}
