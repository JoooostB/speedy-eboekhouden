-- learned_classifications stores per-user "memory" of how the user books
-- recurring transactions. After every successful inbox mutation we compute
-- a normalized signal from the bank line and upsert the chosen booking.
-- On future classifications we look up the signal and either skip Claude
-- entirely (when count >= 2) or feed it as a hint.
--
-- The signal is intentionally narrow (per user, per normalized description
-- + counterparty) so it never crosses tenants. count tracks how many times
-- the user has confirmed this exact mapping; confirmed_at is set on the
-- second matching upsert to avoid one-off misclicks polluting the table.

CREATE TABLE learned_classifications (
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    signal         TEXT NOT NULL,
    grootboekcode  TEXT NOT NULL,
    btw_code       TEXT NOT NULL,
    soort          TEXT NOT NULL,
    count          INTEGER NOT NULL DEFAULT 1,
    sample_omschrijving TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    confirmed_at   TIMESTAMPTZ,
    PRIMARY KEY (user_id, signal)
);

CREATE INDEX idx_learned_user ON learned_classifications(user_id);
