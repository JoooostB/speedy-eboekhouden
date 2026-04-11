-- Encrypted SOAP and REST API credentials for e-boekhouden.
ALTER TABLE user_settings ADD COLUMN IF NOT EXISTS soap_credentials_enc BYTEA;
ALTER TABLE user_settings ADD COLUMN IF NOT EXISTS rest_access_token_enc BYTEA;
