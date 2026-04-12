package handler

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/crypto"
	"github.com/joooostb/speedy-eboekhouden/internal/database"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// EBoekhoudenAuthHandler handles e-boekhouden login (secondary auth within an existing Speedy session).
type EBoekhoudenAuthHandler struct {
	sessions *session.Store
	db       *database.DB
	encKey   crypto.AESKey

	// pendingMFA keeps the e-boekhouden Client in memory during the MFA window.
	// The cookie jar from the login request is needed to complete MFA — it can't
	// be serialized to Redis. Keyed by Speedy session ID, auto-cleaned after 5 min.
	pendingMFA sync.Map
}

type pendingMFAEntry struct {
	Client    *eboekhouden.Client
	ExpiresAt time.Time
}

// NewEBoekhoudenAuthHandler creates a new e-boekhouden auth handler.
func NewEBoekhoudenAuthHandler(sessions *session.Store, db *database.DB, encKey crypto.AESKey) *EBoekhoudenAuthHandler {
	h := &EBoekhoudenAuthHandler{sessions: sessions, db: db, encKey: encKey}
	go h.cleanupPendingMFA()
	return h
}

type ebLoginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Login handles POST /api/v1/eboekhouden/login
func (h *EBoekhoudenAuthHandler) Login(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	var req ebLoginRequest
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
		errMsg := err.Error()
		if strings.Contains(errMsg, "new_ip") {
			// Strip the "new_ip:" prefix so the user only sees the Dutch
			// explanation, not the internal sentinel string.
			cleaned := strings.TrimSpace(strings.TrimPrefix(errMsg, "new_ip:"))
			c.JSON(http.StatusUnauthorized, gin.H{"error": cleaned, "code": "new_ip"})
		} else {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Inloggen bij e-Boekhouden mislukt. Controleer je gegevens."})
		}
		return
	}

	if client.MFARequired {
		// Keep the full client in memory — the cookie jar is needed for MFA completion.
		h.pendingMFA.Store(sess.ID, &pendingMFAEntry{
			Client:    client,
			ExpiresAt: time.Now().Add(5 * time.Minute),
		})

		sess.MFAPending = true
		h.sessions.Update(c.Request.Context(), sess)
		c.JSON(http.StatusOK, gin.H{"status": "mfa_required"})
		return
	}

	token := client.GetAuthToken()
	sess.EBoekhoudenToken = token
	sess.MFAPending = false
	h.sessions.Update(c.Request.Context(), sess)

	// Persist encrypted token to PostgreSQL so it survives Speedy logout/login
	h.persistEBToken(c, sess.UserID, token)

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

type ebMFARequest struct {
	Code string `json:"code" binding:"required"`
}

// MFA handles POST /api/v1/eboekhouden/mfa
func (h *EBoekhoudenAuthHandler) MFA(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	var req ebMFARequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code required"})
		return
	}

	// Retrieve the original client with its cookie jar from the login request
	val, ok := h.pendingMFA.LoadAndDelete(sess.ID)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Geen actieve MFA-aanvraag. Log opnieuw in bij e-Boekhouden."})
		return
	}
	entry := val.(*pendingMFAEntry)
	if time.Now().After(entry.ExpiresAt) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "MFA-aanvraag verlopen. Log opnieuw in bij e-Boekhouden."})
		return
	}

	if err := entry.Client.SubmitMFA(req.Code); err != nil {
		// Put the client back so user can retry with the correct code
		h.pendingMFA.Store(sess.ID, entry)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Verificatie mislukt. Controleer de code en probeer het opnieuw."})
		return
	}

	token := entry.Client.GetAuthToken()
	sess.EBoekhoudenToken = token
	sess.MFAPending = false
	h.sessions.Update(c.Request.Context(), sess)

	// Persist encrypted token to PostgreSQL so it survives Speedy logout/login
	h.persistEBToken(c, sess.UserID, token)

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// persistEBToken encrypts and stores the e-boekhouden token in PostgreSQL.
func (h *EBoekhoudenAuthHandler) persistEBToken(c *gin.Context, userID, token string) {
	if token == "" {
		return
	}
	encrypted, err := crypto.Encrypt(h.encKey, []byte(token))
	if err != nil {
		return // non-fatal — session still works, just won't survive logout
	}
	h.db.SetEBToken(c.Request.Context(), userID, encrypted)
}

// Status handles GET /api/v1/eboekhouden/status
func (h *EBoekhoudenAuthHandler) Status(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"connected":  sess.EBoekhoudenToken != "" && !sess.MFAPending,
		"mfaPending": sess.MFAPending,
	})
}

// Keepalive handles GET /api/v1/eboekhouden/keepalive — pings e-boekhouden to prevent session expiry.
func (h *EBoekhoudenAuthHandler) Keepalive(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	client := session.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusOK, gin.H{"alive": false, "reason": "not_connected"})
		return
	}

	// Make a lightweight API call to keep the e-boekhouden session alive.
	// The user selectlist is small and fast.
	_, err := client.GetEmployees()
	if err != nil {
		// Session expired on e-boekhouden's side — clear it
		sess.EBoekhoudenToken = ""
		sess.MFAPending = false
		h.sessions.Update(c.Request.Context(), sess)
		h.db.ClearEBToken(c.Request.Context(), sess.UserID)
		c.JSON(http.StatusOK, gin.H{"alive": false, "reason": "expired"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"alive": true})
}

// Disconnect handles POST /api/v1/eboekhouden/disconnect
func (h *EBoekhoudenAuthHandler) Disconnect(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	sess.EBoekhoudenToken = ""
	sess.MFAPending = false
	h.sessions.Update(c.Request.Context(), sess)

	// Also clear from PostgreSQL
	h.db.ClearEBToken(c.Request.Context(), sess.UserID)

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *EBoekhoudenAuthHandler) cleanupPendingMFA() {
	ticker := time.NewTicker(1 * time.Minute)
	for range ticker.C {
		now := time.Now()
		h.pendingMFA.Range(func(key, value any) bool {
			entry := value.(*pendingMFAEntry)
			if now.After(entry.ExpiresAt) {
				h.pendingMFA.Delete(key)
			}
			return true
		})
	}
}
