package eboekhouden

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// HourOverviewQuery captures the optional filters for the hour overview
// endpoint. periodStart/periodEnd are required, the rest are 0 = no filter.
type HourOverviewQuery struct {
	PeriodStart  Date // inclusive
	PeriodEnd    Date // inclusive
	MedewerkerID int  // 0 = all employees
	ProjectID    int  // 0 = all projects
	ActiviteitID int  // 0 = all activities
}

// HourOverviewRow is one previously-booked hour entry as returned by
// e-boekhouden's overview grid. Field names match the column metadata
// of /v1/api/uur/overzicht/gridtable.
type HourOverviewRow struct {
	ID               int     `json:"id"`
	Datum            string  `json:"datum"`            // ISO "2026-04-01T00:00:00"
	Medewerker       string  `json:"medewerker"`       // employee display name
	Project          string  `json:"project"`          // project display label
	Activiteit       string  `json:"activiteit"`       // activity display label
	Opmerkingen      string  `json:"opmerkingen"`      // free-text note, often empty
	AantalUren       float64 `json:"aantalUren"`       // booked hours
	AantalKilometers float64 `json:"aantalKilometers"` // booked kilometers (rare)
}

// GetHourOverview queries e-boekhouden for previously-booked hour entries
// in the given period. Used by the bulk-entry calendar to warn the user
// before they accidentally double-book a date that's already filed.
//
// The endpoint paginates at 100 rows per page. We follow the pagination
// because a user querying a whole quarter or year can easily exceed 100.
func (c *Client) GetHourOverview(q HourOverviewQuery) ([]HourOverviewRow, error) {
	const pageSize = 100
	var all []HourOverviewRow
	for offset := 0; ; offset += pageSize {
		page, total, err := c.fetchHourOverviewPage(q, offset, pageSize)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(all) >= total || len(page) == 0 {
			break
		}
	}
	return all, nil
}

// fetchHourOverviewPage retrieves one page of the overview grid and returns
// the parsed rows + the total rowCount so the caller can decide whether to
// continue paginating.
func (c *Client) fetchHourOverviewPage(q HourOverviewQuery, offset, limit int) ([]HourOverviewRow, int, error) {
	params := url.Values{}
	params.Set("offset", strconv.Itoa(offset))
	params.Set("limit", strconv.Itoa(limit))
	params.Set("orderBy", "Datum")
	params.Set("sortDirection", "0")
	params.Set("periodStart", formatCompactDate(q.PeriodStart))
	params.Set("periodEnd", formatCompactDate(q.PeriodEnd))
	if q.MedewerkerID > 0 {
		params.Set("medewerkerId", strconv.Itoa(q.MedewerkerID))
	}
	params.Set("projectId", strconv.Itoa(q.ProjectID))
	params.Set("activiteitId", strconv.Itoa(q.ActiviteitID))

	raw, err := c.apiGet("/v1/api/uur/overzicht/gridtable?" + params.Encode())
	if err != nil {
		return nil, 0, fmt.Errorf("hour overview request: %w", err)
	}

	// The response is the column-indexed grid format e-boekhouden uses
	// across its overview endpoints. We hardcode the column order matching
	// what the API returns (verified via hour-overview.har) rather than
	// reading colMetadata dynamically — that keeps parsing simple and
	// surfaces upstream schema changes loudly.
	var grid struct {
		RowCount int     `json:"rowCount"`
		Data     [][]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &grid); err != nil {
		return nil, 0, fmt.Errorf("parsing hour overview grid: %w", err)
	}

	rows := make([]HourOverviewRow, 0, len(grid.Data))
	for _, r := range grid.Data {
		if len(r) < 8 {
			continue
		}
		rows = append(rows, HourOverviewRow{
			ID:               asInt(r[0]),
			Datum:            asString(r[1]),
			Medewerker:       asString(r[2]),
			Project:          asString(r[3]),
			Activiteit:       asString(r[4]),
			Opmerkingen:      asString(r[5]),
			AantalUren:       asFloat(r[6]),
			AantalKilometers: asFloat(r[7]),
		})
	}
	return rows, grid.RowCount, nil
}

// formatCompactDate renders a Date as YYYYMMDD which is what
// e-boekhouden's hour overview endpoint expects (no separators).
func formatCompactDate(d Date) string {
	return fmt.Sprintf("%04d%02d%02d", d.Year, d.Month, d.Day)
}

func asInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

func asFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	}
	return 0
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

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
