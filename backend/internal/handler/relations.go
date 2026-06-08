package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// SearchKvK handles GET /api/v1/kvk/search?q=...
func SearchKvK(c *gin.Context) {
	client := session.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}

	query := c.Query("q")
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Zoekterm is verplicht"})
		return
	}

	raw, err := client.SearchKvK(query)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	// Parse the grid response into a clean array
	var grid struct {
		Data [][]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &grid); err != nil {
		c.Data(http.StatusOK, "application/json", raw)
		return
	}

	// Map columns: [0]=nr, [1]=bedrijf, [2]=plaats, [3]=straatnaam, [4]=postcode,
	//              [5]=huisnummer, [6]=huisnummerToevoeging, [7]=adres, [8]=vestigingsnummer
	type kvkResult struct {
		KvkNummer        string `json:"kvkNummer"`
		Bedrijf          string `json:"bedrijf"`
		Plaats           string `json:"plaats"`
		Adres            string `json:"adres"`
		Vestigingsnummer string `json:"vestigingsnummer"`
	}

	var results []kvkResult
	for _, row := range grid.Data {
		r := kvkResult{}
		if len(row) > 0 {
			r.KvkNummer, _ = row[0].(string)
		}
		if len(row) > 1 {
			r.Bedrijf, _ = row[1].(string)
		}
		if len(row) > 2 {
			r.Plaats, _ = row[2].(string)
		}
		if len(row) > 7 {
			r.Adres, _ = row[7].(string)
		}
		if len(row) > 8 {
			r.Vestigingsnummer, _ = row[8].(string)
		}
		results = append(results, r)
	}

	c.JSON(http.StatusOK, results)
}

// GetKvKAddress handles GET /api/v1/kvk/address/:vestigingsnummer
func GetKvKAddress(c *gin.Context) {
	client := session.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}

	vestigingsnummer := c.Param("vestigingsnummer")
	raw, err := client.GetKvKAddress(vestigingsnummer)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", raw)
}

// CreateRelation handles POST /api/v1/relations
//
// Returns a structured upstream-validation error as 400, not 502, so
// Cloudflare passes it through to the user instead of swallowing it and
// rendering its branded "Bad Gateway" page (which gives the user zero
// signal about what was wrong with their input). Real gateway failures —
// network errors, e-boekhouden being down — still return 502 so the
// distinction stays meaningful in logs and monitoring.
func CreateRelation(c *gin.Context) {
	client := session.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}

	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	log.Printf("CreateRelation: payload=%s", truncateForLog(string(body), 500))

	raw, err := client.CreateRelation(json.RawMessage(body))
	if err != nil {
		msg := err.Error()
		log.Printf("CreateRelation upstream error: %v", err)
		// Errors prefixed "e-Boekhouden: " come from our parseErrorEnvelope
		// detection — these are application-level validation errors from
		// the upstream API, not gateway failures. Surfacing them as 400
		// lets the actual Dutch error message reach the user (Cloudflare
		// intercepts our 502s and shows its own page).
		if strings.HasPrefix(msg, "e-Boekhouden: ") {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": msg})
		return
	}

	log.Printf("CreateRelation: success")
	c.Data(http.StatusOK, "application/json", raw)
}

// truncateForLog clips s to maxLen characters with an ellipsis suffix.
// Used for log lines that include user payloads so they stay grep-able
// without exploding when someone pastes a huge body.
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...(truncated)"
}
