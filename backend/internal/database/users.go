package database

import (
	"context"
	"fmt"
	"time"
)

// User represents a registered user.
type User struct {
	ID             string    `json:"id"`
	Email          string    `json:"email"`
	Name           string    `json:"name"`
	AvatarKey      string    `json:"avatarKey,omitempty"`
	WebAuthnHandle []byte    `json:"-"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// CreateUser inserts a new user and returns it.
func (db *DB) CreateUser(ctx context.Context, email, name string, webauthnHandle []byte) (*User, error) {
	u := &User{}
	err := db.Pool.QueryRow(ctx,
		`INSERT INTO users (email, name, webauthn_handle) VALUES ($1, $2, $3)
		 RETURNING id, email, name, created_at, updated_at`,
		email, name, webauthnHandle,
	).Scan(&u.ID, &u.Email, &u.Name, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}
	u.WebAuthnHandle = webauthnHandle
	return u, nil
}

// GetUserByID retrieves a user by their UUID.
func (db *DB) GetUserByID(ctx context.Context, id string) (*User, error) {
	u := &User{}
	var avatarKey *string
	err := db.Pool.QueryRow(ctx,
		`SELECT id, email, name, avatar_key, webauthn_handle, created_at, updated_at FROM users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Email, &u.Name, &avatarKey, &u.WebAuthnHandle, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("getting user by id: %w", err)
	}
	if avatarKey != nil {
		u.AvatarKey = *avatarKey
	}
	return u, nil
}

// GetUserByEmail retrieves a user by email address.
func (db *DB) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	u := &User{}
	var avatarKey *string
	err := db.Pool.QueryRow(ctx,
		`SELECT id, email, name, avatar_key, webauthn_handle, created_at, updated_at FROM users WHERE email = $1`,
		email,
	).Scan(&u.ID, &u.Email, &u.Name, &avatarKey, &u.WebAuthnHandle, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("getting user by email: %w", err)
	}
	if avatarKey != nil {
		u.AvatarKey = *avatarKey
	}
	return u, nil
}

// SetAvatarKey updates the user's avatar R2 object key.
func (db *DB) SetAvatarKey(ctx context.Context, userID, key string) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE users SET avatar_key = $1, updated_at = now() WHERE id = $2`,
		key, userID,
	)
	if err != nil {
		return fmt.Errorf("setting avatar key: %w", err)
	}
	return nil
}
