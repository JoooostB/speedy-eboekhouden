package database

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// LearnedClassification is a per-user memory of how a recurring bank line
// should be booked. Built up after every successful inbox mutation.
type LearnedClassification struct {
	UserID             string     `json:"-"`
	Signal             string     `json:"signal"`
	Grootboekcode      string     `json:"grootboekcode"`
	BTWCode            string     `json:"btwCode"`
	Soort              string     `json:"soort"`
	Count              int        `json:"count"`
	SampleOmschrijving string     `json:"sampleOmschrijving"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
	ConfirmedAt        *time.Time `json:"confirmedAt,omitempty"`
}

// signalNormalizer matches anything that's not a letter — used to strip
// digits, dates, punctuation, transaction IDs, etc. from a description so
// only the human-recognizable supplier name remains.
var signalNormalizer = regexp.MustCompile(`[^a-z]+`)

// BuildClassificationSignal turns a bank line into a stable per-user lookup
// key. The signal must be deterministic (so two visits to the same shop
// produce the same key) and discriminative enough that two different
// suppliers don't collide.
//
// Strategy: lowercase the description, strip everything that isn't a letter,
// take the first 30 characters, then suffix with the counterparty IBAN when
// present. The IBAN suffix is what stops "bol.com" colliding with another
// "BOL" merchant on a different account.
func BuildClassificationSignal(omschrijving, tegenrekeningIBAN string) string {
	cleaned := signalNormalizer.ReplaceAllString(strings.ToLower(omschrijving), "")
	if len(cleaned) > 30 {
		cleaned = cleaned[:30]
	}
	iban := strings.ToUpper(strings.ReplaceAll(tegenrekeningIBAN, " ", ""))
	if cleaned == "" && iban == "" {
		return ""
	}
	return cleaned + "|" + iban
}

// LookupLearned returns the learned mapping for a single signal, or nil if
// none exists. Used during inbox classification to short-circuit Claude.
func (db *DB) LookupLearned(ctx context.Context, userID, signal string) (*LearnedClassification, error) {
	if signal == "" {
		return nil, nil
	}
	row := &LearnedClassification{}
	err := db.Pool.QueryRow(ctx,
		`SELECT user_id, signal, grootboekcode, btw_code, soort, count, sample_omschrijving, created_at, updated_at, confirmed_at
		 FROM learned_classifications WHERE user_id = $1 AND signal = $2`,
		userID, signal,
	).Scan(
		&row.UserID, &row.Signal, &row.Grootboekcode, &row.BTWCode, &row.Soort,
		&row.Count, &row.SampleOmschrijving, &row.CreatedAt, &row.UpdatedAt, &row.ConfirmedAt,
	)
	if err != nil {
		// pgx returns a sentinel for "no rows"; we treat any error as "not learned".
		// Callers fall back to Claude either way, so logging here would be noise.
		return nil, nil
	}
	return row, nil
}

// LookupLearnedBatch fetches all learned mappings for a user keyed by signal,
// so the inbox classifier can do an O(1) lookup per bank line without N
// round-trips. Returns an empty map if the user has no memory yet.
func (db *DB) LookupLearnedBatch(ctx context.Context, userID string) (map[string]*LearnedClassification, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT user_id, signal, grootboekcode, btw_code, soort, count, sample_omschrijving, created_at, updated_at, confirmed_at
		 FROM learned_classifications WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying learned: %w", err)
	}
	defer rows.Close()

	out := make(map[string]*LearnedClassification)
	for rows.Next() {
		r := &LearnedClassification{}
		if err := rows.Scan(
			&r.UserID, &r.Signal, &r.Grootboekcode, &r.BTWCode, &r.Soort,
			&r.Count, &r.SampleOmschrijving, &r.CreatedAt, &r.UpdatedAt, &r.ConfirmedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning learned row: %w", err)
		}
		out[r.Signal] = r
	}
	return out, rows.Err()
}

// UpsertLearned records a successful booking. On conflict (same user_id +
// signal), increments the count and updates the booking fields. confirmed_at
// is set on the SECOND matching submission so a single accidental misclick
// doesn't immediately become "trusted".
//
// If the values being upserted differ from what's already stored, the count
// resets to 1 — we treat the new mapping as a fresh choice that needs to be
// re-confirmed before it gets auto-applied.
func (db *DB) UpsertLearned(ctx context.Context, userID, signal, grootboekcode, btwCode, soort, sampleOmschrijving string) error {
	if signal == "" || grootboekcode == "" {
		return nil // nothing useful to learn
	}
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO learned_classifications
		   (user_id, signal, grootboekcode, btw_code, soort, count, sample_omschrijving, confirmed_at)
		 VALUES ($1, $2, $3, $4, $5, 1, $6, NULL)
		 ON CONFLICT (user_id, signal) DO UPDATE SET
		   count = CASE
		     WHEN learned_classifications.grootboekcode = EXCLUDED.grootboekcode
		       AND learned_classifications.btw_code = EXCLUDED.btw_code
		       AND learned_classifications.soort = EXCLUDED.soort
		     THEN learned_classifications.count + 1
		     ELSE 1
		   END,
		   grootboekcode = EXCLUDED.grootboekcode,
		   btw_code = EXCLUDED.btw_code,
		   soort = EXCLUDED.soort,
		   sample_omschrijving = EXCLUDED.sample_omschrijving,
		   updated_at = now(),
		   confirmed_at = CASE
		     WHEN learned_classifications.grootboekcode = EXCLUDED.grootboekcode
		       AND learned_classifications.btw_code = EXCLUDED.btw_code
		       AND learned_classifications.soort = EXCLUDED.soort
		       AND learned_classifications.count >= 1
		     THEN COALESCE(learned_classifications.confirmed_at, now())
		     ELSE NULL
		   END`,
		userID, signal, grootboekcode, btwCode, soort, sampleOmschrijving,
	)
	if err != nil {
		return fmt.Errorf("upserting learned: %w", err)
	}
	return nil
}

// ListLearned returns all learned mappings for a user, ordered by most
// recently updated. Used by the settings UI.
func (db *DB) ListLearned(ctx context.Context, userID string) ([]*LearnedClassification, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT user_id, signal, grootboekcode, btw_code, soort, count, sample_omschrijving, created_at, updated_at, confirmed_at
		 FROM learned_classifications WHERE user_id = $1 ORDER BY updated_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing learned: %w", err)
	}
	defer rows.Close()

	var out []*LearnedClassification
	for rows.Next() {
		r := &LearnedClassification{}
		if err := rows.Scan(
			&r.UserID, &r.Signal, &r.Grootboekcode, &r.BTWCode, &r.Soort,
			&r.Count, &r.SampleOmschrijving, &r.CreatedAt, &r.UpdatedAt, &r.ConfirmedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning learned row: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteLearned removes a single learned mapping. Used by the settings UI
// for "wipe this row".
func (db *DB) DeleteLearned(ctx context.Context, userID, signal string) error {
	_, err := db.Pool.Exec(ctx,
		`DELETE FROM learned_classifications WHERE user_id = $1 AND signal = $2`,
		userID, signal,
	)
	if err != nil {
		return fmt.Errorf("deleting learned: %w", err)
	}
	return nil
}

// DeleteAllLearned wipes a user's entire learning memory. Used by the
// settings UI for "alles wissen".
func (db *DB) DeleteAllLearned(ctx context.Context, userID string) error {
	_, err := db.Pool.Exec(ctx,
		`DELETE FROM learned_classifications WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("deleting all learned: %w", err)
	}
	return nil
}
