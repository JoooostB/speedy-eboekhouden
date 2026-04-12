package eboekhouden

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// extractAuthTokenFromJar checks the cookie jar for an auth-token on known domains.
func (c *Client) extractAuthTokenFromJar() {
	for _, base := range []string{baseURLSecure, baseURLSecure20} {
		u, err := url.Parse(base)
		if err != nil {
			continue
		}
		for _, cookie := range c.httpClient.Jar.Cookies(u) {
			if cookie.Name == "auth-token" && cookie.Value != "" {
				c.SetAuthToken(cookie.Value)
				return
			}
		}
	}
}

// Login performs the initial login to e-boekhouden.
// Returns nil on success. Sets MFARequired if MFA is needed.
func (c *Client) Login(email, password string) error {
	loginURL := baseURLSecure + "/bh/inloggen.asp?LOGIN=1&EBM="

	payload := url.Values{}
	payload.Set("txtEmail", email)
	payload.Set("txtWachtwoord", password)

	req, err := http.NewRequest("POST", loginURL, strings.NewReader(payload.Encode()))
	if err != nil {
		return fmt.Errorf("creating login request: %w", err)
	}

	addCommonHeaders(req)
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Header.Set("cache-control", "max-age=0")
	req.Header.Set("origin", baseURLSecure)
	req.Header.Set("referer", baseURLSecure+"/bh/inloggen.asp")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	// Extract cookies from response and any redirects
	c.extractAuthToken(resp)

	// Follow redirects manually to collect all cookies
	for resp.StatusCode >= 300 && resp.StatusCode < 400 {
		location := resp.Header.Get("Location")
		if location == "" {
			break
		}
		if !strings.HasPrefix(location, "http") {
			location = baseURLSecure + location
		}

		req, err = http.NewRequest("GET", location, nil)
		if err != nil {
			break
		}
		addCommonHeaders(req)
		c.setAuthCookie(req)

		resp, err = c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("following redirect: %w", err)
		}
		defer resp.Body.Close()
		c.extractAuthToken(resp)
	}

	// Read the response body to check for MFA
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// Check if MFA is required by looking for the MFA form
	if strings.Contains(bodyStr, "txtCode") || strings.Contains(bodyStr, "SCODE") {
		c.MFARequired = true
	} else {
		c.MFARequired = false
	}

	// Fallback: check the cookie jar in case the token was set via jar instead of response headers
	if c.GetAuthToken() == "" {
		c.extractAuthTokenFromJar()
	}

	if c.GetAuthToken() == "" && !c.MFARequired {
		// e-boekhouden blocks logins from unknown IP addresses and sends a
		// verification email. Detect this to give the user a clear message.
		if strings.Contains(bodyStr, "IP") || strings.Contains(bodyStr, "bevestig") || strings.Contains(bodyStr, "e-mail") {
			return fmt.Errorf("new_ip: e-Boekhouden heeft een aanmelding vanaf een onbekend IP-adres geblokkeerd. Dit gebeurt wanneer je voor het eerst inlogt vanaf een nieuwe locatie of nieuw netwerk — een beveiligingsmaatregel van e-Boekhouden. Controleer je e-mail (de afzender is e-boekhouden.nl) en klik op de bevestigingslink om dit IP-adres goed te keuren. Probeer daarna opnieuw in te loggen.")
		}
		return fmt.Errorf("login mislukt: geen sessie ontvangen van e-Boekhouden")
	}

	return nil
}

// SubmitMFA submits the MFA code.
func (c *Client) SubmitMFA(code string) error {
	mfaURL := baseURLSecure + "/bh/inloggen.asp?SCODE=1"

	payload := url.Values{}
	payload.Set("txtCode", code)
	payload.Set("submit1", "Verder >")

	req, err := http.NewRequest("POST", mfaURL, strings.NewReader(payload.Encode()))
	if err != nil {
		return fmt.Errorf("creating MFA request: %w", err)
	}

	addCommonHeaders(req)
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Header.Set("origin", baseURLSecure)
	req.Header.Set("referer", baseURLSecure+"/bh/inloggen.asp?SCODE=1")
	c.setAuthCookie(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("MFA request failed: %w", err)
	}
	defer resp.Body.Close()

	c.extractAuthToken(resp)

	// Follow redirects
	for resp.StatusCode >= 300 && resp.StatusCode < 400 {
		location := resp.Header.Get("Location")
		if location == "" {
			break
		}
		if !strings.HasPrefix(location, "http") {
			location = baseURLSecure + location
		}

		req, err = http.NewRequest("GET", location, nil)
		if err != nil {
			break
		}
		addCommonHeaders(req)
		c.setAuthCookie(req)

		resp, err = c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("following MFA redirect: %w", err)
		}
		defer resp.Body.Close()
		c.extractAuthToken(resp)
	}

	// Fallback: check the cookie jar in case the token was set via jar instead of response headers
	if c.GetAuthToken() == "" {
		c.extractAuthTokenFromJar()
	}

	if c.GetAuthToken() == "" {
		return fmt.Errorf("no auth token received after MFA")
	}

	return nil
}

// extractAuthToken looks for the auth-token cookie in response cookies.
func (c *Client) extractAuthToken(resp *http.Response) {
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "auth-token" && cookie.Value != "" {
			c.SetAuthToken(cookie.Value)
			return
		}
	}
}

// setAuthCookie adds the auth-token cookie to a request.
func (c *Client) setAuthCookie(req *http.Request) {
	token := c.GetAuthToken()
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "auth-token", Value: token})
	}
}
