package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/claude"
	"github.com/joooostb/speedy-eboekhouden/internal/crypto"
	"github.com/joooostb/speedy-eboekhouden/internal/database"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// ClassifyHandler handles transaction classification via Claude.
type ClassifyHandler struct {
	claude *claude.Service
	db     *database.DB
	encKey crypto.AESKey
}

// NewClassifyHandler creates a new classify handler.
func NewClassifyHandler(claudeSvc *claude.Service, db *database.DB, encKey crypto.AESKey) *ClassifyHandler {
	return &ClassifyHandler{claude: claudeSvc, db: db, encKey: encKey}
}

// Classify handles POST /api/v1/classify
func (h *ClassifyHandler) Classify(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	// Get API key from database
	settings, err := h.db.GetSettings(c.Request.Context(), sess.UserID)
	if err != nil || !settings.HasAnthropicKey {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no_api_key", "message": "Stel eerst een Anthropic API-sleutel in"})
		return
	}

	apiKey, err := crypto.Decrypt(h.encKey, settings.AnthropicKeyEnc)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decrypt API key"})
		return
	}

	var req claude.ClassifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	result, err := h.claude.ClassifyTransaction(c.Request.Context(), string(apiKey), req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "claude_error", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}
