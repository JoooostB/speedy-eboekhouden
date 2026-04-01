package eboekhouden

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// GetActivities fetches the list of active activities and returns the raw JSON.
func (c *Client) GetActivities() (json.RawMessage, error) {
	reqURL := baseURLSecure20 + "/v1/api/activiteit/getall/true"

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating activities request: %w", err)
	}

	addAPIHeaders(req)
	c.setAuthCookie(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("activities request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading activities response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("activities request returned %d: %s", resp.StatusCode, string(body))
	}

	return json.RawMessage(body), nil
}
