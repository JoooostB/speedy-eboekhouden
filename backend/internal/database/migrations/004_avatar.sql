-- User avatar stored as an R2 object key.
ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar_key TEXT;
