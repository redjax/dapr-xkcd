package postgres

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/001_initial.sql
var initialSchema string

//go:embed migrations/002_retry_queue.sql
var retrySchema string

func Open(ctx context.Context, connectionString string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}

	if err := migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate PostgreSQL: %w", err)
	}

	return pool, nil
}

func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	if _, err := tx.Exec(ctx, `
		SELECT pg_advisory_xact_lock(734812901)
	`); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return err
	}

	migrations := []struct {
		version int
		sql     string
	}{
		{version: 1, sql: initialSchema},
		{version: 2, sql: retrySchema},
	}

	for _, migration := range migrations {
		var applied bool

		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM schema_migrations
				WHERE version = $1
			)
		`, migration.version).Scan(&applied); err != nil {
			return err
		}

		if applied {
			continue
		}

		if _, err := tx.Exec(ctx, migration.sql); err != nil {
			return fmt.Errorf(
				"apply migration %d: %w",
				migration.version,
				err,
			)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO schema_migrations (version) VALUES ($1)
		`, migration.version); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}
