package eboekhouden

import (
	"net/http"
	"net/http/cookiejar"
	"sync"
)

const (
	baseURLSecure   = "https://secure.e-boekhouden.nl"
	baseURLSecure20 = "https://secure20.e-boekhouden.nl"
)

// Client holds an authenticated session with e-boekhouden.
type Client struct {
	httpClient *http.Client
	authToken  string
	mu         sync.RWMutex
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

// addAPIHeaders adds headers for JSON API calls.
func addAPIHeaders(req *http.Request) {
	req.Header.Set("accept", "application/json, text/plain, */*")
	req.Header.Set("accept-language", "nl,en-GB;q=0.9,en;q=0.8")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("dnt", "1")
	req.Header.Set("origin", baseURLSecure20)
	req.Header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36")
}
