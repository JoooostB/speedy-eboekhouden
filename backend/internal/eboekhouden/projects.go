package eboekhouden

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// GetProjects fetches the list of active projects and returns the raw JSON.
func (c *Client) GetProjects() (json.RawMessage, error) {
	reqURL := baseURLSecure20 + "/v1/api/project/rel?byUser=false&userId=NaN&active=true"

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating projects request: %w", err)
	}

	addAPIHeaders(req)
	c.setAuthCookie(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("projects request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading projects response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("projects request returned %d: %s", resp.StatusCode, string(body))
	}

	return json.RawMessage(body), nil
}
