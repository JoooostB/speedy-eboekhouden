-- Store encrypted e-boekhouden auth token so it survives Speedy logout/login cycles.
ALTER TABLE user_settings ADD COLUMN IF NOT EXISTS eb_token_enc BYTEA;
