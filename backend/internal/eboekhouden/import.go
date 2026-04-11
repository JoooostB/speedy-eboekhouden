package eboekhouden

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// GetImportGrid fetches unprocessed bank statement lines.
func (c *Client) GetImportGrid(offset, limit int) (json.RawMessage, error) {
	path := fmt.Sprintf("/v1/api/import/gridtable?offset=%d&limit=%d&orderBy=&sortDirection=1", offset, limit)
	return c.apiGet(path)
}

// GetMutatieByAfschrift gets a pre-fill suggestion for a bank statement line.
func (c *Client) GetMutatieByAfschrift(params url.Values) (json.RawMessage, error) {
	return c.apiGet("/v1/api/mutatie/getmutatiebyafschrift?" + params.Encode())
}

// GetLastMutatieData gets the last used mutation templates.
func (c *Client) GetLastMutatieData() (json.RawMessage, error) {
	return c.apiGet("/v1/api/mutatie/getlastmutatiedata")
}

// ParseImportGrid transforms the column-indexed grid response into named rows.
func ParseImportGrid(raw json.RawMessage) ([]map[string]any, int, error) {
	var grid struct {
		RowCount int     `json:"rowCount"`
		Data     [][]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &grid); err != nil {
		return nil, 0, fmt.Errorf("parsing grid: %w", err)
	}

	// Column mapping based on HAR capture analysis
	fields := []string{"id", "datum", "rekening", "mutDatum", "mutBedrag", "mutOmschrijving", "mutFactuur", "grootboekId", "opmerking", "hasFiles", "verwerkFailureReason"}

	var rows []map[string]any
	for _, row := range grid.Data {
		m := make(map[string]any)
		for i, field := range fields {
			if i < len(row) {
				m[field] = row[i]
			}
		}
		rows = append(rows, m)
	}

	return rows, grid.RowCount, nil
}

// GetImportCount returns just the count of unprocessed lines (via a limit=1 grid query).
func (c *Client) GetImportCount() (int, error) {
	raw, err := c.GetImportGrid(0, 1)
	if err != nil {
		return 0, err
	}
	var grid struct {
		RowCount int `json:"rowCount"`
	}
	if err := json.Unmarshal(raw, &grid); err != nil {
		return 0, fmt.Errorf("parsing count: %w", err)
	}
	return grid.RowCount, nil
}

// BuildAfschriftParams creates URL params for the mutatie suggestion endpoint.
func BuildAfschriftParams(id int, rekening string, mutDatum string, mutBedrag float64, mutOmschrijving string, mutFactuur string, grootboekId int) url.Values {
	v := url.Values{}
	v.Set("id", strconv.Itoa(id))
	v.Set("rekening", rekening)
	v.Set("mutDatum", mutDatum)
	v.Set("mutBedrag", strconv.FormatFloat(mutBedrag, 'f', 2, 64))
	v.Set("mutOmschrijving", mutOmschrijving)
	v.Set("mutFactuur", mutFactuur)
	v.Set("grootboekId", strconv.Itoa(grootboekId))
	if mutBedrag < 0 {
		v.Set("mutSoort", "4") // betaling verstuurd
	} else {
		v.Set("mutSoort", "3") // betaling ontvangen
	}
	return v
}
