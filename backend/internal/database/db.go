package database

import (
	"context"
	"embed"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// DB wraps a PostgreSQL connection pool.
type DB struct {
	Pool *pgxpool.Pool
}

// New creates a new database connection pool and runs migrations.
// Retries the connection up to 10 times with 2-second intervals to handle container startup ordering.
func New(ctx context.Context, dsn string) (*DB, error) {
	var pool *pgxpool.Pool
	var err error

	for attempt := 1; attempt <= 10; attempt++ {
		pool, err = pgxpool.New(ctx, dsn)
		if err != nil {
			log.Printf("Postgres connection attempt %d/10 failed: %v", attempt, err)
			time.Sleep(2 * time.Second)
			continue
		}

		if err = pool.Ping(ctx); err != nil {
			pool.Close()
			log.Printf("Postgres ping attempt %d/10 failed: %v", attempt, err)
			time.Sleep(2 * time.Second)
			continue
		}

		break
	}

	if err != nil {
		return nil, fmt.Errorf("connecting to postgres after 10 attempts: %w", err)
	}

	db := &DB{Pool: pool}
	if err := db.migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return db, nil
}

// Close shuts down the connection pool.
func (db *DB) Close() {
	db.Pool.Close()
}

// migrate runs all SQL migration files in order.
func (db *DB) migrate(ctx context.Context) error {
	_, err := db.Pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("reading migrations dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		version := strings.TrimSuffix(entry.Name(), ".sql")

		var exists bool
		err := db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)", version).Scan(&exists)
		if err != nil {
			return fmt.Errorf("checking migration %s: %w", version, err)
		}
		if exists {
			continue
		}

		sql, err := migrationFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", entry.Name(), err)
		}

		tx, err := db.Pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("beginning transaction for %s: %w", version, err)
		}

		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("executing migration %s: %w", version, err)
		}

		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("recording migration %s: %w", version, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("committing migration %s: %w", version, err)
		}
	}

	return nil
}
