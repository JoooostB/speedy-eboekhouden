package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/database"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// LearnedSettingsHandler exposes endpoints for users to view and reset the
// "memory" of recurring transactions Speedy has learned from their bookings.
type LearnedSettingsHandler struct {
	db *database.DB
}

// NewLearnedSettingsHandler creates the handler.
func NewLearnedSettingsHandler(db *database.DB) *LearnedSettingsHandler {
	return &LearnedSettingsHandler{db: db}
}

// List handles GET /api/v1/settings/learned — returns all learned mappings
// for the current user, ordered by most recently updated.
func (h *LearnedSettingsHandler) List(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	rows, err := h.db.ListLearned(c.Request.Context(), sess.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Geleerde boekingen laden mislukt"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"learned": rows})
}

// Delete handles DELETE /api/v1/settings/learned/:signal — wipes a single
// learned mapping. The signal is passed via query parameter rather than the
// path because it can contain characters that are awkward in URLs.
func (h *LearnedSettingsHandler) Delete(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	signal := c.Query("signal")
	if signal == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "signal is verplicht"})
		return
	}

	if err := h.db.DeleteLearned(c.Request.Context(), sess.UserID, signal); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Verwijderen mislukt"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// DeleteAll handles DELETE /api/v1/settings/learned — wipes the user's
// entire learning memory. Requires the explicit confirm=true query param
// as a UX safeguard against accidental wipes from misrouted frontend code.
//
// NOTE: confirm=true is NOT a CSRF token. CSRF protection comes from the
// SameSite=Lax session cookie + the CORS FRONTEND_ORIGIN allowlist; an
// attacker who could already trigger this DELETE could trivially append the
// query parameter. Don't add new "security" features keyed off this guard.
func (h *LearnedSettingsHandler) DeleteAll(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	if c.Query("confirm") != "true" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "confirm=true is verplicht"})
		return
	}

	if err := h.db.DeleteAllLearned(c.Request.Context(), sess.UserID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Wissen mislukt"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
