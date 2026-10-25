package session

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
)

const (
	CookieName       = "speedy-session"
	ContextKey       = "session"
	ClientContextKey = "ebClient"
)

// CookieOptions are the deployment-dependent attributes of the session cookie.
type CookieOptions struct {
	Domain string
	Secure bool
}

// SetCookie issues the session cookie for id, valid for maxAge. Login and
// the middleware both go through here, so a refresh never changes the
// cookie's attributes.
func SetCookie(c *gin.Context, id string, maxAge time.Duration, opts CookieOptions) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(CookieName, id, int(maxAge.Seconds()), "/", opts.Domain, opts.Secure, true)
}

// Middleware validates the session cookie via Redis and injects session + e-boekhouden client into context.
// Every authenticated request re-issues the cookie with the full max age, in
// step with the sliding Redis TTL; without that the browser dropped the
// cookie maxAge after login, however active the user was.
func Middleware(store *Store, opts CookieOptions) gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie(CookieName)
		if err != nil || cookie == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
			return
		}

		sess, err := store.Get(c.Request.Context(), cookie)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "session lookup failed"})
			return
		}
		if sess == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "session expired"})
			return
		}

		c.Set(ContextKey, sess)
		SetCookie(c, cookie, store.maxAge, opts)

		// Reconstruct e-boekhouden client from stored auth token (only when MFA is complete)
		if sess.EBoekhoudenToken != "" && !sess.MFAPending {
			client, err := eboekhouden.NewClientWithToken(sess.EBoekhoudenToken)
			if err == nil {
				c.Set(ClientContextKey, client)
			}
		}

		c.Next()
	}
}

// FromContext extracts the session data from the Gin context.
func FromContext(c *gin.Context) *SessionData {
	val, exists := c.Get(ContextKey)
	if !exists {
		return nil
	}
	return val.(*SessionData)
}

// ClientFromContext extracts the e-boekhouden client from the Gin context.
func ClientFromContext(c *gin.Context) *eboekhouden.Client {
	val, exists := c.Get(ClientContextKey)
	if !exists {
		return nil
	}
	return val.(*eboekhouden.Client)
}

// RequireEBoekhouden is middleware that ensures an e-boekhouden connection exists.
func RequireEBoekhouden() gin.HandlerFunc {
	return func(c *gin.Context) {
		if ClientFromContext(c) == nil {
			c.AbortWithStatusJSON(http.StatusPreconditionFailed, gin.H{
				"error":   "eboekhouden_not_connected",
				"message": "Koppel Speedy eerst aan e-Boekhouden.",
			})
			return
		}
		c.Next()
	}
}
