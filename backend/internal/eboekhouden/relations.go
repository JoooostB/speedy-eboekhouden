package eboekhouden

import (
	"encoding/json"
	"net/url"
	"strconv"
)

// GetLastRelation returns the last created relation (for code suggestion).
func (c *Client) GetLastRelation() (json.RawMessage, error) {
	return c.apiGet("/v1/api/relatie/last?la=false")
}

// GetRelation returns one relation's full model (GET /v1/api/relatie/{id}),
// the call e-boekhouden's own relation screen makes. Unlike the search hit it
// always carries grootboekrekeningId, the relation's default ledger account.
func (c *Client) GetRelation(id int) (json.RawMessage, error) {
	return c.apiGet("/v1/api/relatie/" + strconv.Itoa(id))
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
