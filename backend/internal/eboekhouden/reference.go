package eboekhouden

import (
	"encoding/json"
	"net/url"
)

// GetActiveLedgerAccounts fetches active grootboekrekeningen with internal IDs.
func (c *Client) GetActiveLedgerAccounts() (json.RawMessage, error) {
	return c.apiGet("/v1/api/grootboekrekening/selectlist/actieveGBR")
}

// SearchRelations searches relations by query string.
func (c *Client) SearchRelations(query string) (json.RawMessage, error) {
	return c.apiGet("/v1/api/relatie/search?query=" + url.QueryEscape(query))
}

// GetBTWInfo fetches all VAT codes and percentages.
func (c *Client) GetBTWInfo() (json.RawMessage, error) {
	return c.apiGet("/v1/api/btwinfo/info")
}
