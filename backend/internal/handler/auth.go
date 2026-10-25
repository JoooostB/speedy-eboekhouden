package handler

import (
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/crypto"
	"github.com/joooostb/speedy-eboekhouden/internal/database"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
	"github.com/redis/go-redis/v9"
)

// EBoekhoudenAuthHandler handles e-boekhouden login (secondary auth within an existing Speedy session).
type EBoekhoudenAuthHandler struct {
	sessions *session.Store
	db       *database.DB
	encKey   crypto.AESKey
	// redis holds the per-user caches that Disconnect purges (see
	// userCachePatterns). nil skips the purge; tests pass a fake.
	redis keyScanner
	// logoutEB ends the e-boekhouden session behind a token and reports
	// whether e-boekhouden confirmed that the token is dead. Defaults to
	// logoutEBoekhouden; tests replace it.
	logoutEB func(token string) bool
	// logoutTimeout bounds how long Disconnect waits for logoutEB; zero
	// means disconnectLogoutTimeout. Tests shorten it.
	logoutTimeout time.Duration

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
// redisClient may be nil, in which case Disconnect clears the token but
// purges no caches.
func NewEBoekhoudenAuthHandler(sessions *session.Store, db *database.DB, encKey crypto.AESKey, redisClient *redis.Client) *EBoekhoudenAuthHandler {
	h := &EBoekhoudenAuthHandler{sessions: sessions, db: db, encKey: encKey, logoutEB: logoutEBoekhouden}
	if redisClient != nil {
		h.redis = redisClient
	}
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
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Inloggen bij e-Boekhouden is mislukt. Controleer je e-mailadres en wachtwoord en probeer het opnieuw."})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "Er is geen aanmelding meer die op deze code wacht. Log opnieuw in met je e-mailadres en wachtwoord."})
		return
	}
	entry := val.(*pendingMFAEntry)
	if time.Now().After(entry.ExpiresAt) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "De 5 minuten voor de verificatiecode zijn voorbij. Log opnieuw in met je e-mailadres en wachtwoord."})
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

// Disconnect handles POST /api/v1/eboekhouden/disconnect ("Verbinding
// verbreken"). In order:
//
//  1. Best-effort: log the token out at e-boekhouden and check whether the
//     session is really gone; that result is ebLoggedOut.
//  2. Clear the token from the Redis session and from PostgreSQL. A pending
//     MFA login is dropped as well.
//  3. Purge this user's cached administration data from Redis
//     (userCachePatterns): classification, analyses, duplicate markers,
//     locks and the ignore list. Learned classifications stay: they are the
//     user's preferences, deletable under Instellingen.
//
// A failure in step 1 or 3 never prevents step 2. The response is
// {status:"ok", ebLoggedOut:bool, purgedKeys:n}; ebLoggedOut is true only
// when e-boekhouden reported the token as invalid after the logout request.
func (h *EBoekhoudenAuthHandler) Disconnect(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	ctx := c.Request.Context()

	loggedOut := false
	if sess.EBoekhoudenToken != "" && !sess.MFAPending && h.logoutEB != nil {
		timeout := h.logoutTimeout
		if timeout <= 0 {
			timeout = disconnectLogoutTimeout
		}
		loggedOut = logoutWithin(h.logoutEB, sess.EBoekhoudenToken, timeout)
	}
	h.pendingMFA.Delete(sess.ID)

	sess.EBoekhoudenToken = ""
	sess.MFAPending = false
	if h.sessions != nil {
		h.sessions.Update(ctx, sess)
	}
	if h.db != nil {
		h.db.ClearEBToken(ctx, sess.UserID)
	}

	purged := 0
	if h.redis != nil {
		n, err := purgeUserCaches(ctx, h.redis, sess.UserID)
		purged = n
		if err != nil {
			log.Printf("Disconnect: cache purge incomplete for user %s after %d keys: %v", sess.UserID, n, err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":      "ok",
		"ebLoggedOut": loggedOut,
		"purgedKeys":  purged,
	})
}

// disconnectLogoutTimeout bounds the logout at e-boekhouden in Disconnect.
// The token is cleared locally either way, so a stalled e-boekhouden must not
// keep the "Verbinding verbreken" dialog spinning: after this long the answer
// is ebLoggedOut:false (honest, not confirmed) and the logout finishes in the
// background, itself bounded by the client's request timeout.
const disconnectLogoutTimeout = 15 * time.Second

// logoutWithin runs logout for token and returns its result, or false when
// it takes longer than d. The channel is buffered so a late logout can still
// deliver its result and end.
func logoutWithin(logout func(string) bool, token string, d time.Duration) bool {
	done := make(chan bool, 1)
	go func() { done <- logout(token) }()
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case ok := <-done:
		return ok
	case <-timer.C:
		return false
	}
}

// logoutEBoekhouden is the production logoutEB: it sends the logout request
// for token and returns true only when e-boekhouden then reports the token as
// no longer valid. The logout page answers with the login page whether or not
// anything happened, so the follow-up check is what counts. Only error
// classes are logged.
func logoutEBoekhouden(token string) bool {
	client, err := eboekhouden.NewClientWithToken(token)
	if err != nil {
		return false
	}
	if err := client.Logout(); err != nil {
		log.Printf("Disconnect: e-boekhouden logout request failed: %s", errClass(err))
	}
	valid, err := client.CheckAuth()
	if err != nil {
		log.Printf("Disconnect: e-boekhouden auth check failed: %s", errClass(err))
		return false
	}
	return !valid
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
