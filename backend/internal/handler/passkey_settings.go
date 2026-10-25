package handler

import (
	"encoding/base64"
	"net/http"
	"strings"
	"unicode/utf8"

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

// maxPasskeyNameLength is the longest friendly name in characters (runes),
// the unit the message and the frontend's maxLength speak of.
const maxPasskeyNameLength = 64

// passkeyName trims a requested friendly name and checks it. It returns the
// name to store, or a Dutch message saying why it cannot be used. The limit
// counts characters, not bytes: "Sleutel van Zoë" must not lose room for the
// ë, which takes two bytes in UTF-8.
func passkeyName(raw string) (string, string) {
	name := strings.TrimSpace(raw)
	switch {
	case name == "":
		return "", "Geef de passkey een naam."
	case utf8.RuneCountInString(name) > maxPasskeyNameLength:
		return "", "De naam mag maximaal 64 tekens lang zijn."
	}
	return name, ""
}

type passkeyListItem struct {
	ID           string   `json:"id"`
	FriendlyName string   `json:"friendlyName"`
	CreatedAt    string   `json:"createdAt"`
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ongeldig passkey-id."})
		return
	}

	var req struct {
		FriendlyName string `json:"friendlyName"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Geef de passkey een naam."})
		return
	}
	name, problem := passkeyName(req.FriendlyName)
	if problem != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": problem})
		return
	}

	if err := h.db.RenameCredential(c.Request.Context(), credID, sess.UserID, name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Hernoemen is mislukt. Probeer het opnieuw."})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ongeldig passkey-id."})
		return
	}

	creds, err := h.db.GetCredentialsByUserID(c.Request.Context(), sess.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Je passkeys konden niet worden geladen, dus er is niets verwijderd. Probeer het opnieuw."})
		return
	}
	if len(creds) <= 1 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Je laatste passkey kun je niet verwijderen, want zonder passkey kun je niet meer inloggen.",
		})
		return
	}

	if err := h.db.DeleteCredential(c.Request.Context(), credID, sess.UserID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Verwijderen is mislukt. Probeer het opnieuw."})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
