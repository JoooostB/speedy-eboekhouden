package database

import (
	"context"
	"encoding/json"
	"fmt"
)

// UserSettings holds a user's stored preferences and encrypted credentials.
type UserSettings struct {
	UserID             string          `json:"userId"`
	AnthropicKeyEnc    []byte          `json:"-"`
	HasAnthropicKey    bool            `json:"hasAnthropicKey"`
	EBTokenEnc         []byte          `json:"-"`
	HasEBToken         bool            `json:"hasEbToken"`
	SoapCredentialsEnc []byte          `json:"-"`
	HasSoapCredentials bool            `json:"hasSoapCredentials"`
	RestAccessTokenEnc []byte          `json:"-"`
	HasRestAccessToken bool            `json:"hasRestAccessToken"`
	Preferences        json.RawMessage `json:"preferences"`
}

// GetSettings retrieves user settings, returning defaults if none exist.
func (db *DB) GetSettings(ctx context.Context, userID string) (*UserSettings, error) {
	s := &UserSettings{UserID: userID}
	var keyEnc, ebTokenEnc, soapEnc, restEnc []byte
	err := db.Pool.QueryRow(ctx,
		`SELECT anthropic_api_key_enc, eb_token_enc, soap_credentials_enc, rest_access_token_enc, preferences
		 FROM user_settings WHERE user_id = $1`,
		userID,
	).Scan(&keyEnc, &ebTokenEnc, &soapEnc, &restEnc, &s.Preferences)
	if err != nil {
		s.Preferences = json.RawMessage(`{}`)
		return s, nil
	}
	s.AnthropicKeyEnc = keyEnc
	s.HasAnthropicKey = len(keyEnc) > 0
	s.EBTokenEnc = ebTokenEnc
	s.HasEBToken = len(ebTokenEnc) > 0
	s.SoapCredentialsEnc = soapEnc
	s.HasSoapCredentials = len(soapEnc) > 0
	s.RestAccessTokenEnc = restEnc
	s.HasRestAccessToken = len(restEnc) > 0
	return s, nil
}

// SetAnthropicKey stores the encrypted API key.
func (db *DB) SetAnthropicKey(ctx context.Context, userID string, encryptedKey []byte) error {
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO user_settings (user_id, anthropic_api_key_enc)
		 VALUES ($1, $2)
		 ON CONFLICT (user_id) DO UPDATE SET anthropic_api_key_enc = EXCLUDED.anthropic_api_key_enc`,
		userID, encryptedKey,
	)
	if err != nil {
		return fmt.Errorf("setting anthropic key: %w", err)
	}
	return nil
}

// DeleteAnthropicKey removes the stored API key.
func (db *DB) DeleteAnthropicKey(ctx context.Context, userID string) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE user_settings SET anthropic_api_key_enc = NULL WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("deleting anthropic key: %w", err)
	}
	return nil
}

// SetEBToken stores the encrypted e-boekhouden auth token.
func (db *DB) SetEBToken(ctx context.Context, userID string, encryptedToken []byte) error {
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO user_settings (user_id, eb_token_enc)
		 VALUES ($1, $2)
		 ON CONFLICT (user_id) DO UPDATE SET eb_token_enc = EXCLUDED.eb_token_enc`,
		userID, encryptedToken,
	)
	if err != nil {
		return fmt.Errorf("setting eb token: %w", err)
	}
	return nil
}

// ClearEBToken removes the stored e-boekhouden token.
func (db *DB) ClearEBToken(ctx context.Context, userID string) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE user_settings SET eb_token_enc = NULL WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("clearing eb token: %w", err)
	}
	return nil
}

// SetSoapCredentials stores the encrypted SOAP credentials.
func (db *DB) SetSoapCredentials(ctx context.Context, userID string, encrypted []byte) error {
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO user_settings (user_id, soap_credentials_enc)
		 VALUES ($1, $2)
		 ON CONFLICT (user_id) DO UPDATE SET soap_credentials_enc = EXCLUDED.soap_credentials_enc`,
		userID, encrypted,
	)
	if err != nil {
		return fmt.Errorf("setting soap credentials: %w", err)
	}
	return nil
}

// DeleteSoapCredentials removes the stored SOAP credentials.
func (db *DB) DeleteSoapCredentials(ctx context.Context, userID string) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE user_settings SET soap_credentials_enc = NULL WHERE user_id = $1`,
		userID,
	)
	return err
}

// SetRestAccessToken stores the encrypted REST access token.
func (db *DB) SetRestAccessToken(ctx context.Context, userID string, encrypted []byte) error {
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO user_settings (user_id, rest_access_token_enc)
		 VALUES ($1, $2)
		 ON CONFLICT (user_id) DO UPDATE SET rest_access_token_enc = EXCLUDED.rest_access_token_enc`,
		userID, encrypted,
	)
	if err != nil {
		return fmt.Errorf("setting rest access token: %w", err)
	}
	return nil
}

// DeleteRestAccessToken removes the stored REST access token.
func (db *DB) DeleteRestAccessToken(ctx context.Context, userID string) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE user_settings SET rest_access_token_enc = NULL WHERE user_id = $1`,
		userID,
	)
	return err
}

// SetPreferences stores user preferences as JSON.
func (db *DB) SetPreferences(ctx context.Context, userID string, prefs json.RawMessage) error {
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO user_settings (user_id, preferences)
		 VALUES ($1, $2)
		 ON CONFLICT (user_id) DO UPDATE SET preferences = EXCLUDED.preferences`,
		userID, prefs,
	)
	if err != nil {
		return fmt.Errorf("setting preferences: %w", err)
	}
	return nil
}
