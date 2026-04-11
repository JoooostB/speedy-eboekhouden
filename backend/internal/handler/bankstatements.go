package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// GetBankStatements handles GET /api/v1/bankstatements
func GetBankStatements(c *gin.Context) {
	client := session.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}

	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "2000"))

	raw, err := client.GetImportGrid(offset, limit)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	// Parse grid format into named rows for the frontend
	rows, totalCount, err := eboekhouden.ParseImportGrid(raw)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse grid: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items":      rows,
		"totalCount": totalCount,
	})
}

// GetBankStatementCount handles GET /api/v1/bankstatements/count
func GetBankStatementCount(c *gin.Context) {
	client := session.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}

	count, err := client.GetImportCount()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"count": count})
}

// GetBankStatementSuggestion handles GET /api/v1/bankstatements/:id/suggestion
func GetBankStatementSuggestion(c *gin.Context) {
	client := session.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}

	id, _ := strconv.Atoi(c.Param("id"))
	params := eboekhouden.BuildAfschriftParams(
		id,
		c.Query("rekening"),
		c.Query("mutDatum"),
		parseFloat(c.Query("mutBedrag")),
		c.Query("mutOmschrijving"),
		c.Query("mutFactuur"),
		parseInt(c.Query("grootboekId")),
	)

	raw, err := client.GetMutatieByAfschrift(params)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", raw)
}

// ProcessBankStatement handles POST /api/v1/bankstatements/:id/process
func ProcessBankStatement(c *gin.Context) {
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

	raw, err := client.CreateMutatie(json.RawMessage(body))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", raw)
}

// GetLastMutatieData handles GET /api/v1/bankstatements/lastdata
func GetLastMutatieData(c *gin.Context) {
	client := session.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}

	raw, err := client.GetLastMutatieData()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", raw)
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func parseInt(s string) int {
	i, _ := strconv.Atoi(s)
	return i
}
