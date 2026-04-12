package eboekhouden

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// Client holds an authenticated session with e-boekhouden.
type Client struct {
	httpClient  *http.Client
	authToken   string
	mu          sync.RWMutex
	MFARequired bool
}

// NewClient creates a new unauthenticated client with a cookie jar.
func NewClient() (*Client, error) {
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
	}, nil
}

// NewClientWithToken creates a client with an existing auth token (from a Redis session).
func NewClientWithToken(token string) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	c := &Client{
		httpClient: &http.Client{
			Jar: jar,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		authToken: token,
	}
	return c, nil
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
	req, err := http.NewRequest("GET", baseURLSecure20+path, nil)
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
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request to %s returned %d: %s", path, resp.StatusCode, string(body))
	}
	return json.RawMessage(body), nil
}

// apiPost performs a POST request to the secure20 API with a JSON body and returns the raw JSON response.
func (c *Client) apiPost(path string, payload json.RawMessage) (json.RawMessage, error) {
	req, err := http.NewRequest("POST", baseURLSecure20+path, bytes.NewReader(payload))
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
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("request to %s returned %d: %s", path, resp.StatusCode, string(body))
	}
	return json.RawMessage(body), nil
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
