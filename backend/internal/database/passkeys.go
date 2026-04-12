package database

import (
	"context"
	"fmt"
	"time"
)

// PasskeyCredential represents a stored WebAuthn credential.
type PasskeyCredential struct {
	ID              []byte    `json:"id"`
	UserID          string    `json:"userId"`
	PublicKey       []byte    `json:"publicKey"`
	AttestationType string    `json:"attestationType"`
	AAGUID          []byte    `json:"aaguid"`
	Transport       []string  `json:"transport"`
	SignCount       uint32    `json:"signCount"`
	CreatedAt       time.Time `json:"createdAt"`
	FriendlyName    string    `json:"friendlyName"`
	// WebAuthn credential flags. BackupEligible is the critical one — it
	// must remain immutable across the credential's lifetime, so we have to
	// store it at registration and pass it back at login. UserPresent and
	// UserVerified are stored for completeness; they're not strictly required
	// by go-webauthn's discoverable login validator but help downstream code
	// reason about the credential.
	BackupEligible bool `json:"backupEligible"`
	BackupState    bool `json:"backupState"`
	UserPresent    bool `json:"userPresent"`
	UserVerified   bool `json:"userVerified"`
}

// StoreCredential saves a new passkey credential.
func (db *DB) StoreCredential(ctx context.Context, cred *PasskeyCredential) error {
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO passkey_credentials
		   (id, user_id, public_key, attestation_type, aaguid, transport, sign_count, friendly_name,
		    backup_eligible, backup_state, user_present, user_verified)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		cred.ID, cred.UserID, cred.PublicKey, cred.AttestationType, cred.AAGUID, cred.Transport, cred.SignCount, cred.FriendlyName,
		cred.BackupEligible, cred.BackupState, cred.UserPresent, cred.UserVerified,
	)
	if err != nil {
		return fmt.Errorf("storing credential: %w", err)
	}
	return nil
}

// GetCredentialsByUserID returns all passkey credentials for a user.
func (db *DB) GetCredentialsByUserID(ctx context.Context, userID string) ([]*PasskeyCredential, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, user_id, public_key, attestation_type, aaguid, transport, sign_count, created_at, friendly_name,
		        backup_eligible, backup_state, user_present, user_verified
		 FROM passkey_credentials WHERE user_id = $1 ORDER BY created_at`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying credentials: %w", err)
	}
	defer rows.Close()

	var creds []*PasskeyCredential
	for rows.Next() {
		c := &PasskeyCredential{}
		if err := rows.Scan(&c.ID, &c.UserID, &c.PublicKey, &c.AttestationType, &c.AAGUID, &c.Transport, &c.SignCount, &c.CreatedAt, &c.FriendlyName,
			&c.BackupEligible, &c.BackupState, &c.UserPresent, &c.UserVerified); err != nil {
			return nil, fmt.Errorf("scanning credential: %w", err)
		}
		creds = append(creds, c)
	}
	return creds, rows.Err()
}

// GetCredentialByID returns a single credential by its ID.
func (db *DB) GetCredentialByID(ctx context.Context, credID []byte) (*PasskeyCredential, error) {
	c := &PasskeyCredential{}
	err := db.Pool.QueryRow(ctx,
		`SELECT id, user_id, public_key, attestation_type, aaguid, transport, sign_count, created_at, friendly_name,
		        backup_eligible, backup_state, user_present, user_verified
		 FROM passkey_credentials WHERE id = $1`,
		credID,
	).Scan(&c.ID, &c.UserID, &c.PublicKey, &c.AttestationType, &c.AAGUID, &c.Transport, &c.SignCount, &c.CreatedAt, &c.FriendlyName,
		&c.BackupEligible, &c.BackupState, &c.UserPresent, &c.UserVerified)
	if err != nil {
		return nil, fmt.Errorf("getting credential: %w", err)
	}
	return c, nil
}

// UpdateSignCount updates the sign counter for a credential.
func (db *DB) UpdateSignCount(ctx context.Context, credID []byte, count uint32) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE passkey_credentials SET sign_count = $1 WHERE id = $2`,
		count, credID,
	)
	if err != nil {
		return fmt.Errorf("updating sign count: %w", err)
	}
	return nil
}

// RenameCredential updates the friendly_name for a credential. The user_id
// check ensures users can only rename their own credentials.
func (db *DB) RenameCredential(ctx context.Context, credID []byte, userID, name string) error {
	res, err := db.Pool.Exec(ctx,
		`UPDATE passkey_credentials SET friendly_name = $1 WHERE id = $2 AND user_id = $3`,
		name, credID, userID,
	)
	if err != nil {
		return fmt.Errorf("renaming credential: %w", err)
	}
	if res.RowsAffected() == 0 {
		return fmt.Errorf("credential not found")
	}
	return nil
}

// DeleteCredential removes a passkey credential.
func (db *DB) DeleteCredential(ctx context.Context, credID []byte, userID string) error {
	_, err := db.Pool.Exec(ctx,
		`DELETE FROM passkey_credentials WHERE id = $1 AND user_id = $2`,
		credID, userID,
	)
	if err != nil {
		return fmt.Errorf("deleting credential: %w", err)
	}
	return nil
}
