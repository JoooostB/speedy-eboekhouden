package handler

import (
	"encoding/json"
	"net/http"

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

	raw, err := client.CreateRelation(json.RawMessage(body))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", raw)
}
