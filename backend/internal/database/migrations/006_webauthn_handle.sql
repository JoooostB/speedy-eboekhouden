-- Store the WebAuthn user handle so login can match it.
ALTER TABLE users ADD COLUMN IF NOT EXISTS webauthn_handle BYTEA;
