package eboekhouden

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// SubmitHourEntry submits a single hour entry to e-boekhouden.
func (c *Client) SubmitHourEntry(entry HourEntry) error {
	reqURL := baseURLSecure20 + "/v1/api/uur"

	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling hour entry: %w", err)
	}

	req, err := http.NewRequest("POST", reqURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("creating hour request: %w", err)
	}

	addAPIHeaders(req)
	c.setAuthCookie(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("hour submission failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("hour submission returned %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
