package database

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

const recoveryTokenTTL = 15 * time.Minute

// CreateRecoveryToken generates a one-time recovery token for a user.
// Returns the raw token (to send via email). Only the hash is stored.
func (db *DB) CreateRecoveryToken(ctx context.Context, userID string) (string, error) {
	// Generate random token
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}
	rawToken := hex.EncodeToString(b)

	// Store hash only
	hash := hashToken(rawToken)
	expiresAt := time.Now().Add(recoveryTokenTTL)

	// Invalidate any existing tokens for this user
	db.Pool.Exec(ctx, `DELETE FROM recovery_tokens WHERE user_id = $1`, userID)

	_, err := db.Pool.Exec(ctx,
		`INSERT INTO recovery_tokens (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		hash, userID, expiresAt,
	)
	if err != nil {
		return "", fmt.Errorf("storing recovery token: %w", err)
	}

	return rawToken, nil
}

// ValidateRecoveryToken atomically checks and consumes a recovery token.
// Returns the user ID if the token is valid, unused, and not expired.
func (db *DB) ValidateRecoveryToken(ctx context.Context, rawToken string) (string, error) {
	hash := hashToken(rawToken)

	var userID string
	err := db.Pool.QueryRow(ctx,
		`UPDATE recovery_tokens
		 SET used = true
		 WHERE token_hash = $1 AND used = false AND expires_at > NOW()
		 RETURNING user_id`,
		hash,
	).Scan(&userID)
	if err != nil {
		return "", fmt.Errorf("ongeldige, verlopen of reeds gebruikte link")
	}

	return userID, nil
}

func hashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}
