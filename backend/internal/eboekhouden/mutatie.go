package eboekhouden

import (
	"encoding/json"
	"fmt"
)

// CreateMutatie creates a new mutation via the web API.
// The payload is passed through from the frontend.
func (c *Client) CreateMutatie(payload json.RawMessage) (json.RawMessage, error) {
	return c.apiPost("/v1/api/mutatie", payload)
}

// LinkFileToMutation links an archive file to a mutation via POST /v1/api/folderkoppel/koppel/item.
func (c *Client) LinkFileToMutation(payload json.RawMessage) (json.RawMessage, error) {
	return c.apiPost("/v1/api/folderkoppel/koppel/item", payload)
}

// GetMutatieModel fetches a booked mutation by its number via
// GET /v1/api/mutatie/nr/{mutNr}/model: {mutatie:{nr, relatieId, datum, soort,
// factuur, …}, mutatieRegels:[{bedragExclusief, btw, bedragInclusief, …}],
// folderKoppelId}. The open-post grid carries the mutNr of every outstanding
// invoice, so this is how the invoice behind an open post is read. Use
// ParseMutatieTotals for the amounts.
func (c *Client) GetMutatieModel(mutNr int) (json.RawMessage, error) {
	if mutNr <= 0 {
		return nil, fmt.Errorf("invalid mutatie number %d", mutNr)
	}
	return c.apiGet(fmt.Sprintf("/v1/api/mutatie/nr/%d/model", mutNr))
}

// MutatieTotals are the amounts of a mutation summed over its lines: the
// amount excluding BTW and the BTW itself. For a sales invoice these are the
// figures a G-rekening split (WKA) is computed from.
type MutatieTotals struct {
	Exclusief float64
	BTW       float64
}

// ParseMutatieTotals sums bedragExclusief and btw over the mutatieRegels of a
// mutation model as returned by GetMutatieModel.
func ParseMutatieTotals(raw json.RawMessage) (MutatieTotals, error) {
	var model struct {
		MutatieRegels []struct {
			BedragExclusief float64 `json:"bedragExclusief"`
			BTW             float64 `json:"btw"`
		} `json:"mutatieRegels"`
	}
	if err := json.Unmarshal(raw, &model); err != nil {
		return MutatieTotals{}, fmt.Errorf("parsing mutatie model: %w", err)
	}
	var t MutatieTotals
	for _, r := range model.MutatieRegels {
		t.Exclusief += r.BedragExclusief
		t.BTW += r.BTW
	}
	return t, nil
}
