package eboekhouden

import (
	"encoding/json"
	"fmt"
)

// Values for the soort parameter of GetOpenPostGrid.
const (
	// OpenPostCrediteur selects outstanding purchase invoices (what we owe).
	OpenPostCrediteur = "Crediteur"
	// OpenPostDebiteur selects outstanding sales invoices (what we are owed).
	OpenPostDebiteur = "Debiteur"
)

// openPostGridLimit is the page size requested from the open-post grid. One
// page is enough for the administrations we target; paging can be added when
// a rowCount larger than this shows up in practice.
const openPostGridLimit = 500

// OpenPost is one outstanding invoice from the open-post grid. Contact details
// present in the grid (address, phone, e-mail) are deliberately not mapped:
// nothing downstream needs them.
type OpenPost struct {
	MutDatum        string  `json:"mutDatum"`
	SBedrag         float64 `json:"sBedrag"`
	MutFactuur      string  `json:"mutFactuur"`
	MutNr           int     `json:"mutNr"`
	RelatieCode     string  `json:"relatieCode"`
	RelatieID       int     `json:"relatieId"`
	Bedrijf         string  `json:"bedrijf"`
	SBetaald        float64 `json:"sBetaald"`
	TeOntvangen     float64 `json:"teOntvangen"`
	Vervaldatum     string  `json:"vervaldatum"`
	MutOmschrijving string  `json:"mutOmschrijving"`
	MutSoort        int     `json:"mutSoort"`
}

// GetOpenPostGrid fetches outstanding invoices as a column-indexed grid. soort
// must be OpenPostCrediteur or OpenPostDebiteur. Use ParseOpenPostGrid to turn
// the result into typed rows.
func (c *Client) GetOpenPostGrid(soort string) (json.RawMessage, error) {
	if soort != OpenPostCrediteur && soort != OpenPostDebiteur {
		return nil, fmt.Errorf("invalid open post soort %q", soort)
	}
	path := fmt.Sprintf("/v1/api/openpost/gridtable/openpost?offset=0&limit=%d&orderBy=&sortDirection=1&soort=%s", openPostGridLimit, soort)
	return c.apiGet(path)
}

// ParseOpenPostGrid transforms the column-indexed grid response into typed
// rows. Unlike ParseImportGrid it resolves columns through colMetadata, so a
// reordered or extended grid keeps working as long as the column names do.
func ParseOpenPostGrid(raw json.RawMessage) ([]OpenPost, error) {
	var grid struct {
		ColMetadata []struct {
			Name  string `json:"name"`
			Index int    `json:"index"`
		} `json:"colMetadata"`
		Data [][]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &grid); err != nil {
		return nil, fmt.Errorf("parsing open post grid: %w", err)
	}
	if len(grid.Data) > 0 && len(grid.ColMetadata) == 0 {
		return nil, fmt.Errorf("parsing open post grid: no column metadata")
	}

	columns := make(map[string]int, len(grid.ColMetadata))
	for _, col := range grid.ColMetadata {
		columns[col.Name] = col.Index
	}

	posts := make([]OpenPost, 0, len(grid.Data))
	for _, row := range grid.Data {
		cell := func(name string) any {
			i, ok := columns[name]
			if !ok || i < 0 || i >= len(row) {
				return nil
			}
			return row[i]
		}
		posts = append(posts, OpenPost{
			MutDatum:        gridString(cell("mutDatum")),
			SBedrag:         gridFloat(cell("sBedrag")),
			MutFactuur:      gridString(cell("mutFactuur")),
			MutNr:           gridInt(cell("mutNr")),
			RelatieCode:     gridString(cell("relatieCode")),
			RelatieID:       gridInt(cell("relatieId")),
			Bedrijf:         gridString(cell("bedrijf")),
			SBetaald:        gridFloat(cell("sBetaald")),
			TeOntvangen:     gridFloat(cell("teOntvangen")),
			Vervaldatum:     gridString(cell("vervaldatum")),
			MutOmschrijving: gridString(cell("mutOmschrijving")),
			MutSoort:        gridInt(cell("mutSoort")),
		})
	}
	return posts, nil
}

// gridString returns a grid cell as a string; null and non-strings become "".
func gridString(v any) string {
	s, _ := v.(string)
	return s
}

// gridFloat returns a grid cell as a float64; null and non-numbers become 0.
// JSON numbers always decode to float64 inside an any.
func gridFloat(v any) float64 {
	f, _ := v.(float64)
	return f
}

// gridInt returns a grid cell as an int, truncating any fractional part.
func gridInt(v any) int {
	return int(gridFloat(v))
}
