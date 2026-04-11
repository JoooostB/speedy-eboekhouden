-- Initial schema for Speedy e-Boekhouden SaaS platform.

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email      TEXT UNIQUE NOT NULL,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE passkey_credentials (
    id               BYTEA PRIMARY KEY,
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    public_key       BYTEA NOT NULL,
    attestation_type TEXT NOT NULL DEFAULT 'none',
    aaguid           BYTEA,
    transport        TEXT[],
    sign_count       BIGINT NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    friendly_name    TEXT
);
CREATE INDEX idx_passkey_user ON passkey_credentials(user_id);

CREATE TABLE teams (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    owner_id   UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE team_members (
    team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role    TEXT NOT NULL DEFAULT 'member',
    PRIMARY KEY (team_id, user_id)
);

CREATE TABLE user_settings (
    user_id               UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    anthropic_api_key_enc BYTEA,
    preferences           JSONB NOT NULL DEFAULT '{}'
);
