package eboekhouden

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// logoutPath is the logout page on the secure host. Requesting it with the
// auth-token cookie ends the e-boekhouden session behind that token; the
// response is the HTML login page.
const logoutPath = "/bh/uitloggen.asp"

// logoutBaseURL is the origin the logout request goes to: the login host
// (baseURLSecure, the same host auth.go logs in on). It is a variable only so
// the package tests can point it at an httptest.Server; production code never
// assigns it and there is no environment override.
var logoutBaseURL = baseURLSecure

// authCheckPath reports whether an auth token is still accepted:
// {"valid":true,"user":{…}} while logged in, {"valid":false,"currentDomain":0}
// after a logout.
const authCheckPath = "/v1/api/auth"

// Logout asks e-boekhouden to end the session behind the client's auth token.
// A nil error only means the request completed (a 2xx, or a redirect to the
// login page); it is not proof that the session is gone, because the logout
// page answers with the login page either way. Callers follow up with
// CheckAuth to learn whether the token is really dead.
func (c *Client) Logout() error {
	req, err := http.NewRequest(http.MethodGet, logoutBaseURL+logoutPath, nil)
	if err != nil {
		return fmt.Errorf("creating logout request: %w", err)
	}
	addCommonHeaders(req)
	c.setAuthCookie(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("logout request failed: %w", err)
	}
	defer resp.Body.Close()
	// The body is the login page; drain a bounded amount so the connection
	// can be reused, and never keep or log it.
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("logout returned %d", resp.StatusCode)
	}
	return nil
}

// CheckAuth reports whether e-boekhouden still accepts the client's auth
// token. A logged-out or expired session is (false, nil), whether e-boekhouden
// says so with valid:false or with its usual session-expired signals (401,
// SECURITY_010). Only a request or decoding failure is an error.
//
// The response carries the account's name and e-mail address in "user". Only
// "valid" is read; the rest is never kept or logged.
func (c *Client) CheckAuth() (bool, error) {
	raw, err := c.apiPost(authCheckPath, nil)
	if errors.Is(err, ErrSessionExpired) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var res struct {
		Valid bool `json:"valid"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return false, fmt.Errorf("parsing auth check response: %w", err)
	}
	return res.Valid, nil
}
