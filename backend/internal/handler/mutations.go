package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// CreateMutation handles POST /api/v1/mutations
func CreateMutation(c *gin.Context) {
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
