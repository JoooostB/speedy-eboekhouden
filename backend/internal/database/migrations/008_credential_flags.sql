-- WebAuthn requires credential flags (BackupEligible, BackupState, UserPresent,
-- UserVerified) to be tracked across registration and login. The library
-- enforces that BackupEligible is immutable for the credential's lifetime —
-- if we don't store it, every login of a syncable passkey (iCloud Keychain,
-- Bitwarden, 1Password, Google Password Manager) fails with "Backup Eligible
-- flag inconsistency detected" because the in-memory zero default never
-- matches the BE=true flag the authenticator returns.
--
-- Defaults are FALSE so the migration is non-destructive on existing rows,
-- but those existing rows will continue to fail login until the user
-- re-registers. Document this in the upgrade notes.

ALTER TABLE passkey_credentials
    ADD COLUMN backup_eligible BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN backup_state    BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN user_present    BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN user_verified   BOOLEAN NOT NULL DEFAULT TRUE;
