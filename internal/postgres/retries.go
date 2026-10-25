package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/redjax/dapr-xkcd/internal/archive"
)

func (s *Store) QueueBackfillFailure(
	ctx context.Context,
	id int,
	message string,
	progress archive.Progress,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	result, err := tx.Exec(ctx, `
		UPDATE comic_fetches
		SET status = CASE
				WHEN retry_attempts >= 5 THEN 'failed'
				ELSE 'retry_pending'
			END,
			last_error = $2,
			next_retry_at = CASE
				WHEN retry_attempts >= 5 THEN NULL
				ELSE now() + interval '5 minutes'
			END
		WHERE comic_id = $1
	`, id, message)

	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("queue failure: comic %d has no attempt row", id)
	}

	if _, err := tx.Exec(ctx, progressSQL,
		progress.LatestSeenID,
		progress.LatestSavedID,
		progress.NextID,
	); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Store) ClaimRetry(ctx context.Context) (int, error) {
	// A worker may have crashed during its fifth attempt.
	// Once that claim expires, stop scheduling further attempts.
	if _, err := s.pool.Exec(ctx, `
		UPDATE comic_fetches
		SET status = 'failed',
			next_retry_at = NULL
		WHERE status = 'retry_pending'
			AND retry_attempts >= 5
			AND next_retry_at <= now()
	`); err != nil {
		return 0, err
	}

	var id int

	err := s.pool.QueryRow(ctx, `
		WITH candidate AS (
			SELECT comic_id
			FROM comic_fetches
			WHERE status = 'retry_pending'
				AND retry_attempts < 5
				AND next_retry_at <= now()
			ORDER BY next_retry_at, comic_id
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE comic_fetches AS f
		SET retry_attempts = f.retry_attempts + 1,
			attempts = f.attempts + 1,
			last_attempt_at = now(),
			next_retry_at = now() + interval '10 minutes'
		FROM candidate
		WHERE f.comic_id = candidate.comic_id
		RETURNING f.comic_id
	`).Scan(&id)

	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}

	return id, err
}

func (s *Store) FinishRetryFailure(
	ctx context.Context,
	id int,
	message string,
) error {
	result, err := s.pool.Exec(ctx, `
		UPDATE comic_fetches
		SET status = CASE
				WHEN retry_attempts >= 5 THEN 'failed'
				ELSE 'retry_pending'
			END,
			last_error = $2,
			next_retry_at = CASE
				WHEN retry_attempts >= 5 THEN NULL
				WHEN retry_attempts = 1 THEN now() + interval '15 minutes'
				WHEN retry_attempts = 2 THEN now() + interval '1 hour'
				WHEN retry_attempts = 3 THEN now() + interval '6 hours'
				ELSE now() + interval '24 hours'
			END
		WHERE comic_id = $1
			AND status = 'retry_pending'
	`, id, message)

	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("record retry failure: comic %d has no active retry", id)
	}

	return nil
}

func (s *Store) SaveRetryComplete(
	ctx context.Context,
	comic archive.Comic,
	image archive.Image,
) error {
	return s.saveComplete(ctx, comic, image, nil)
}
