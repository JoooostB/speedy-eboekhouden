package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/config"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

type AuthHandler struct {
	store  *session.Store
	config *config.Config
}

func NewAuthHandler(store *session.Store, cfg *config.Config) *AuthHandler {
	return &AuthHandler{store: store, config: cfg}
}

type loginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Login handles POST /api/v1/login
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email and password required"})
		return
	}

	client, err := eboekhouden.NewClient()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create client"})
		return
	}

	if err := client.Login(req.Email, req.Password); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "login failed: " + err.Error()})
		return
	}

	sess := h.store.Create(client)
	h.setSessionCookie(c, sess.ID)

	if client.MFARequired {
		c.JSON(http.StatusOK, gin.H{"status": "mfa_required"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

type mfaRequest struct {
	Code string `json:"code" binding:"required"`
}

// MFA handles POST /api/v1/mfa
func (h *AuthHandler) MFA(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}

	var req mfaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code required"})
		return
	}

	if err := sess.Client.SubmitMFA(req.Code); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Logout handles POST /api/v1/logout
func (h *AuthHandler) Logout(c *gin.Context) {
	cookie, err := c.Cookie(session.CookieName)
	if err == nil && cookie != "" {
		h.store.Delete(cookie)
	}

	c.SetCookie(session.CookieName, "", -1, "/", h.config.CookieDomain, h.config.CookieSecure, true)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Me handles GET /api/v1/me — checks if session is still valid
func (h *AuthHandler) Me(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *AuthHandler) setSessionCookie(c *gin.Context, sessionID string) {
	c.SetCookie(
		session.CookieName,
		sessionID,
		h.config.SessionMaxAge*60, // seconds
		"/",
		h.config.CookieDomain,
		h.config.CookieSecure,
		true, // HttpOnly
	)
}
