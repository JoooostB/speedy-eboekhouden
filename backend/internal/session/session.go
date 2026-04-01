package session

import (
	"time"

	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
)

// Session holds a user's authenticated e-boekhouden client.
type Session struct {
	ID        string
	Client    *eboekhouden.Client
	CreatedAt time.Time
	LastUsed  time.Time
}

// Touch updates the last used timestamp.
func (s *Session) Touch() {
	s.LastUsed = time.Now()
}

// IsExpired checks if the session has exceeded the max age.
func (s *Session) IsExpired(maxAgeMinutes int) bool {
	return time.Since(s.LastUsed) > time.Duration(maxAgeMinutes)*time.Minute
}
