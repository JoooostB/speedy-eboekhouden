package session

import "time"

// SessionData holds serializable session state stored in Redis.
type SessionData struct {
	ID               string    `json:"id"`
	UserID           string    `json:"userId"`
	TeamID           string    `json:"teamId,omitempty"`
	EBoekhoudenToken string    `json:"ebToken,omitempty"`
	MFAPending       bool      `json:"mfaPending,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
}
