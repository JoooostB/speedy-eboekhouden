package eboekhouden

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// GetEmployees fetches the list of employees and returns the raw JSON.
func (c *Client) GetEmployees() (json.RawMessage, error) {
	reqURL := baseURLSecure20 + "/v1/api/user/selectlist"

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating employees request: %w", err)
	}

	addAPIHeaders(req)
	c.setAuthCookie(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("employees request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading employees response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("employees request returned %d: %s", resp.StatusCode, string(body))
	}

	return json.RawMessage(body), nil
}
