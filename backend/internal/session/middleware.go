package session

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	CookieName = "speedy-session"
	ContextKey = "session"
)

// Middleware validates the session cookie and injects the session into the Gin context.
func Middleware(store *Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie(CookieName)
		if err != nil || cookie == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
			return
		}

		sess := store.Get(cookie)
		if sess == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "session expired"})
			return
		}

		c.Set(ContextKey, sess)
		c.Next()
	}
}

// FromContext extracts the session from the Gin context.
func FromContext(c *gin.Context) *Session {
	val, exists := c.Get(ContextKey)
	if !exists {
		return nil
	}
	return val.(*Session)
}
