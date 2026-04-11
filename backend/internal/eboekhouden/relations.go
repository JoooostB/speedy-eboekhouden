package eboekhouden

import (
	"encoding/json"
	"net/url"
)

// GetLastRelation returns the last created relation (for code suggestion).
func (c *Client) GetLastRelation() (json.RawMessage, error) {
	return c.apiGet("/v1/api/relatie/last?la=false")
}

// SearchKvK searches the KvK register by number or company name.
func (c *Client) SearchKvK(query string) (json.RawMessage, error) {
	return c.apiGet("/v1/api/kvk/gridtable?search=" + url.QueryEscape(query))
}

// GetKvKAddress gets the full address for a KvK vestiging.
func (c *Client) GetKvKAddress(vestigingsnummer string) (json.RawMessage, error) {
	return c.apiGet("/v1/api/kvk/vestiging/" + url.PathEscape(vestigingsnummer) + "/adres")
}

// CreateRelation creates a new relation in e-boekhouden.
func (c *Client) CreateRelation(payload json.RawMessage) (json.RawMessage, error) {
	return c.apiPost("/v1/api/relatie/model", payload)
}
