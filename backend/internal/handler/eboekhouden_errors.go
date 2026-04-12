package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/database"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// HandleEBoekhoudenSessionExpired inspects err and, if it is
// eboekhouden.ErrSessionExpired, clears the stored e-boekhouden token from
// both the Redis session and the persisted PostgreSQL copy, then writes a
// typed 412 response so the frontend can prompt the user to reconnect.
//
// Returns true if the error was handled (and the response was written), false
// otherwise. Callers should `return` immediately when this returns true.
//
// sessions and db may be nil — in that case the response is still written but
// the underlying token is not cleared (a subsequent /me call will pick up the
// stale state on its own once a handler that *does* have access cleans up).
func HandleEBoekhoudenSessionExpired(
	c *gin.Context,
	sess *session.SessionData,
	sessions *session.Store,
	db *database.DB,
	err error,
) bool {
	if !errors.Is(err, eboekhouden.ErrSessionExpired) {
		return false
	}
	if sess != nil {
		sess.EBoekhoudenToken = ""
		sess.MFAPending = false
		if sessions != nil {
			sessions.Update(c.Request.Context(), sess)
		}
		if db != nil {
			db.ClearEBToken(c.Request.Context(), sess.UserID)
		}
	}
	// Drop the e-boekhouden client from the request context as well so any
	// downstream middleware sees the disconnected state.
	c.Set(session.ClientContextKey, nil)
	c.JSON(http.StatusPreconditionFailed, gin.H{
		"error":   "eboekhouden_session_expired",
		"message": "Je e-Boekhouden sessie is verlopen. Verbind opnieuw om door te gaan.",
	})
	return true
}
