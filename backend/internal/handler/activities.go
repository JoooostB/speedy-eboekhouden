package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// GetActivities handles GET /api/v1/activities
func GetActivities(c *gin.Context) {
	client := session.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}

	raw, err := client.GetActivities()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", raw)
}
