package handler

import (
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/database"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// PasskeySettingsHandler exposes endpoints for users to manage their stored
// passkeys: listing them and renaming the friendly_name shown in the UI.
type PasskeySettingsHandler struct {
	db *database.DB
}

// NewPasskeySettingsHandler creates the handler.
func NewPasskeySettingsHandler(db *database.DB) *PasskeySettingsHandler {
	return &PasskeySettingsHandler{db: db}
}

type passkeyListItem struct {
	ID           string `json:"id"`
	FriendlyName string `json:"friendlyName"`
	CreatedAt    string `json:"createdAt"`
	Transport    []string `json:"transport"`
}

// List handles GET /api/v1/settings/passkeys — returns all passkeys for the
// current user. The credential ID is base64url-encoded so it can be used as a
// URL parameter for the rename and delete endpoints.
func (h *PasskeySettingsHandler) List(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	creds, err := h.db.GetCredentialsByUserID(c.Request.Context(), sess.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load passkeys"})
		return
	}

	items := make([]passkeyListItem, 0, len(creds))
	for _, cr := range creds {
		items = append(items, passkeyListItem{
			ID:           base64.RawURLEncoding.EncodeToString(cr.ID),
			FriendlyName: cr.FriendlyName,
			CreatedAt:    cr.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			Transport:    cr.Transport,
		})
	}
	c.JSON(http.StatusOK, gin.H{"passkeys": items})
}

// Rename handles PATCH /api/v1/settings/passkeys/:id — updates the friendly_name.
func (h *PasskeySettingsHandler) Rename(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	credID, err := base64.RawURLEncoding.DecodeString(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ongeldige passkey id"})
		return
	}

	var req struct {
		FriendlyName string `json:"friendlyName"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "naam is verplicht"})
		return
	}
	name := strings.TrimSpace(req.FriendlyName)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "naam mag niet leeg zijn"})
		return
	}
	if len(name) > 64 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "naam is te lang (max 64 tekens)"})
		return
	}

	if err := h.db.RenameCredential(c.Request.Context(), credID, sess.UserID, name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "passkey hernoemen mislukt"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Delete handles DELETE /api/v1/settings/passkeys/:id — removes a passkey.
// Refuses to delete the last remaining passkey to prevent lockout.
func (h *PasskeySettingsHandler) Delete(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	credID, err := base64.RawURLEncoding.DecodeString(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ongeldige passkey id"})
		return
	}

	creds, err := h.db.GetCredentialsByUserID(c.Request.Context(), sess.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "passkeys laden mislukt"})
		return
	}
	if len(creds) <= 1 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Je kunt je laatste passkey niet verwijderen — je zou jezelf buitensluiten.",
		})
		return
	}

	if err := h.db.DeleteCredential(c.Request.Context(), credID, sess.UserID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "passkey verwijderen mislukt"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
