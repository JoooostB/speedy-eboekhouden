package eboekhouden

import "encoding/json"

// CreateMutatie creates a new mutation via the web API.
// The payload is passed through from the frontend.
func (c *Client) CreateMutatie(payload json.RawMessage) (json.RawMessage, error) {
	return c.apiPost("/v1/api/mutatie", payload)
}

// LinkFileToMutation links an archive file to a mutation via POST /v1/api/folderkoppel/koppel/item.
func (c *Client) LinkFileToMutation(payload json.RawMessage) (json.RawMessage, error) {
	return c.apiPost("/v1/api/folderkoppel/koppel/item", payload)
}
