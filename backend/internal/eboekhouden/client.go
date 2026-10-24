package eboekhouden

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
)

const (
	baseURLSecure   = "https://secure.e-boekhouden.nl"
	baseURLSecure20 = "https://secure20.e-boekhouden.nl"
)

// ErrSessionExpired is returned by api calls when e-boekhouden has invalidated
// the session cookie (typically because too much time has passed since login).
// Handlers should clear the stored token and prompt the user to reconnect.
var ErrSessionExpired = errors.New("eboekhouden session expired")

// isSessionExpiredBody returns true when the response body indicates that the
// e-boekhouden session is no longer valid. e-Boekhouden returns HTTP 200 with
// a JSON error envelope for these, so we have to sniff the body.
func isSessionExpiredBody(status int, body []byte) bool {
	if status == http.StatusUnauthorized {
		return true
	}
	s := string(body)
	return strings.Contains(s, "SECURITY_010") ||
		strings.Contains(s, "Niet ingelogd") ||
		strings.Contains(s, "\"errorType\":\"security\"")
}

// errorEnvelope mirrors the JSON shape e-boekhouden returns for application
// errors: HTTP 200, body like {"errorType":"validation","message":"...","code":"..."}.
// We use json.Unmarshal rather than string sniffing so we can also surface the
// human-readable message back to the caller.
type errorEnvelope struct {
	ErrorType string `json:"errorType"`
	Message   string `json:"message"`
	Code      string `json:"code"`
}

// parseErrorEnvelope returns (envelope, true) when the response body is an
// e-boekhouden error envelope. It is intentionally strict: it requires
// errorType to be set to a non-empty string AND the JSON to NOT contain a
// success marker (mutNr, etc.) so legitimate responses are never misclassified.
//
// Session-expired bodies are excluded because the caller already short-circuits
// those via isSessionExpiredBody (which returns ErrSessionExpired).
func parseErrorEnvelope(body []byte) (errorEnvelope, bool) {
	var env errorEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return env, false
	}
	if env.ErrorType == "" {
		return env, false
	}
	// "security" envelopes are handled separately as ErrSessionExpired so the
	// frontend can flip the connection state. Don't double-handle them here.
	if env.ErrorType == "security" {
		return env, false
	}
	return env, true
}

// Client holds an authenticated session with e-boekhouden.
type Client struct {
	httpClient *http.Client
	authToken  string
	// baseURL is the secure20 API origin used by the api* helpers. It is set
	// at construction and never changes afterwards; tests point it at an
	// httptest.Server via setBaseURL.
	baseURL     string
	mu          sync.RWMutex
	MFARequired bool
}

// newClient builds a client with a cookie jar, redirects disabled and the
// production API base URL. All exported constructors go through here so the
// base URL default cannot be forgotten.
func newClient(token string) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	return &Client{
		httpClient: &http.Client{
			Jar: jar,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		authToken: token,
		baseURL:   baseURLSecure20,
	}, nil
}

// NewClient creates a new unauthenticated client with a cookie jar.
func NewClient() (*Client, error) {
	return newClient("")
}

// NewClientWithToken creates a client with an existing auth token (from a Redis session).
func NewClientWithToken(token string) (*Client, error) {
	return newClient(token)
}

// setBaseURL overrides the API origin. Only tests use this, to talk to an
// httptest.Server instead of e-boekhouden.
func (c *Client) setBaseURL(u string) {
	c.baseURL = u
}

// SetAuthToken stores the given auth token.
func (c *Client) SetAuthToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.authToken = token
}

// GetAuthToken returns the current auth token.
func (c *Client) GetAuthToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.authToken
}

// addCommonHeaders adds browser-like headers to a request.
func addCommonHeaders(req *http.Request) {
	req.Header.Set("accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("accept-language", "nl,en-GB;q=0.9,en;q=0.8")
	req.Header.Set("dnt", "1")
	req.Header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36")
}

// apiGet performs a GET request to the secure20 API and returns the raw JSON response.
func (c *Client) apiGet(path string) (json.RawMessage, error) {
	return c.apiDo(http.MethodGet, path, nil)
}

// apiPost performs a POST request to the secure20 API with a JSON body and returns the raw JSON response.
func (c *Client) apiPost(path string, payload json.RawMessage) (json.RawMessage, error) {
	return c.apiDo(http.MethodPost, path, payload)
}

// apiPut performs a PUT request to the secure20 API with a JSON body and returns the raw JSON response.
func (c *Client) apiPut(path string, payload json.RawMessage) (json.RawMessage, error) {
	return c.apiDo(http.MethodPut, path, payload)
}

// apiDelete performs a DELETE request to the secure20 API and returns the raw JSON response.
func (c *Client) apiDelete(path string) (json.RawMessage, error) {
	return c.apiDo(http.MethodDelete, path, nil)
}

// apiDo performs a JSON request against the secure20 API and applies the error
// handling shared by every verb: session-expired detection, the e-boekhouden
// error envelope (HTTP 200 with errorType) and non-2xx statuses. payload may
// be nil for body-less requests.
func (c *Client) apiDo(method, path string, payload json.RawMessage) (json.RawMessage, error) {
	var reqBody io.Reader
	if payload != nil {
		reqBody = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("creating request for %s: %w", path, err)
	}
	addAPIHeaders(req)
	c.setAuthCookie(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", path, err)
	}
	if isSessionExpiredBody(resp.StatusCode, body) {
		return nil, ErrSessionExpired
	}
	if env, isErr := parseErrorEnvelope(body); isErr {
		return nil, newEnvelopeError(path, resp.StatusCode, env, len(body))
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		// Status and length only: the body may be anything e-boekhouden
		// renders and must not end up in an error that gets logged.
		return nil, newStatusError(path, resp.StatusCode, len(body))
	}
	return json.RawMessage(body), nil
}

// apiGetBinary performs a GET request for a file rather than JSON. It reads at
// most maxBytes and fails when the response is larger, so a huge upload can
// never be buffered in full. e-Boekhouden reports errors for file endpoints
// the same way as for JSON endpoints (HTTP 401, or HTTP 200 with a JSON
// envelope), so those are mapped to the usual errors before the body is
// treated as file content. filename comes from Content-Disposition and is
// empty when that header is missing or unparseable.
func (c *Client) apiGetBinary(path string, maxBytes int64) (data []byte, contentType, filename string, err error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, "", "", fmt.Errorf("creating request for %s: %w", path, err)
	}
	addAPIHeaders(req)
	c.setAuthCookie(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", "", fmt.Errorf("request to %s failed: %w", path, err)
	}
	defer resp.Body.Close()

	// Read one byte past the limit so an oversize file is detectable without
	// ever buffering more than maxBytes+1.
	data, err = io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, "", "", fmt.Errorf("reading response from %s: %w", path, err)
	}

	contentType = resp.Header.Get("Content-Type")
	// Only sniff the body for error markers when it is not the file itself: a
	// PDF or image is never an error envelope, and scanning megabytes of
	// binary for "SECURITY_010" would be both wasteful and a false-positive
	// risk.
	if resp.StatusCode != http.StatusOK || isJSONContentType(contentType) {
		if isSessionExpiredBody(resp.StatusCode, data) {
			return nil, "", "", ErrSessionExpired
		}
		if env, isErr := parseErrorEnvelope(data); isErr {
			return nil, "", "", newEnvelopeError(path, resp.StatusCode, env, len(data))
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", "", newStatusError(path, resp.StatusCode, len(data))
	}
	if int64(len(data)) > maxBytes {
		return nil, "", "", fmt.Errorf("file from %s exceeds %d bytes", path, maxBytes)
	}
	return data, contentType, contentDispositionFilename(resp.Header.Get("Content-Disposition")), nil
}

// isJSONContentType reports whether a Content-Type header denotes JSON,
// ignoring parameters such as charset.
func isJSONContentType(header string) bool {
	mediaType, _, err := mime.ParseMediaType(header)
	if err != nil {
		return false
	}
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

// contentDispositionFilename extracts the filename from a Content-Disposition
// header such as "attachment; filename=a.pdf" with an optional RFC 2231/5987
// filename* parameter (percent-encoded UTF-8). mime.ParseMediaType decodes
// filename* into the plain "filename" key after the plain one has been read,
// so the UTF-8 variant wins whenever both are present. Any directory part is
// dropped because the value is upstream input. A missing or malformed header
// yields "": a download without a usable name is still a successful download.
func contentDispositionFilename(header string) string {
	if header == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(header)
	if err != nil {
		return ""
	}
	name := params["filename"]
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// envelopeMessage returns the most user-friendly string from an error
// envelope, capped by capMessage.
func envelopeMessage(env errorEnvelope) string {
	msg := env.Message
	if msg == "" {
		msg = env.Code
	}
	if msg == "" {
		msg = env.ErrorType
	}
	return capMessage(msg)
}

// capMessage truncates upstream message text to 200 characters. The cap
// prevents an unusually long or maliciously crafted upstream message from
// inflating our API responses or causing rendering issues in the frontend.
func capMessage(msg string) string {
	const maxLen = 200
	if len(msg) > maxLen {
		msg = msg[:maxLen]
	}
	return msg
}

// addAPIHeaders adds headers for JSON API calls.
func addAPIHeaders(req *http.Request) {
	req.Header.Set("accept", "application/json, text/plain, */*")
	req.Header.Set("accept-language", "nl,en-GB;q=0.9,en;q=0.8")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("dnt", "1")
	req.Header.Set("origin", baseURLSecure20)
	req.Header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36")
}
