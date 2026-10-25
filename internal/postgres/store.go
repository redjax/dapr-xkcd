package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redjax/dapr-xkcd/internal/archive"
)

type Store struct {
	pool *pgxpool.Pool
}

var _ archive.Store = (*Store)(nil)

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) GetComic(ctx context.Context, id int) (archive.Comic, error) {
	var comic archive.Comic
	var data []byte

	err := s.pool.QueryRow(ctx,
		`SELECT metadata FROM comics WHERE id = $1`,
		id,
	).Scan(&data)

	if errors.Is(err, pgx.ErrNoRows) {
		return comic, archive.ErrNotFound
	}
	if err != nil {
		return comic, err
	}

	if err := json.Unmarshal(data, &comic); err != nil {
		return comic, err
	}

	comic.Metadata = json.RawMessage(data)
	return comic, nil
}

func (s *Store) GetImage(ctx context.Context, id int) (archive.Image, error) {
	var image archive.Image

	err := s.pool.QueryRow(ctx, `
		SELECT data, content_type, sha256
		FROM comic_images
		WHERE comic_id = $1
	`, id).Scan(&image.Data, &image.ContentType, &image.SHA256)

	if errors.Is(err, pgx.ErrNoRows) {
		return image, archive.ErrNotFound
	}

	return image, err
}

func (s *Store) LoadProgress(ctx context.Context) (archive.Progress, error) {
	var progress archive.Progress

	err := s.pool.QueryRow(ctx, `
		SELECT latest_seen_id, latest_saved_id, backfill_next_id, updated_at
		FROM sync_state
		WHERE name = 'xkcd'
	`).Scan(
		&progress.LatestSeenID,
		&progress.LatestSavedID,
		&progress.NextID,
		&progress.UpdatedAt,
	)

	return progress, err
}

func (s *Store) IsComplete(ctx context.Context, id int) (bool, error) {
	var complete bool

	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM comic_fetches f
			JOIN comics c ON c.id = f.comic_id
			JOIN comic_images i ON i.comic_id = c.id
			WHERE f.comic_id = $1
				AND f.status = 'complete'
		)
	`, id).Scan(&complete)

	return complete, err
}

func (s *Store) RetryManaged(ctx context.Context, id int) (bool, error) {
	var managed bool

	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM comic_fetches
			WHERE comic_id = $1
				AND status IN ('retry_pending', 'failed', 'unavailable')
		)
	`, id).Scan(&managed)

	return managed, err
}

func (s *Store) ObserveLatest(ctx context.Context, id int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sync_state
		SET latest_seen_id = GREATEST(latest_seen_id, $1),
			updated_at = now()
		WHERE name = 'xkcd'
	`, id)

	return err
}

func (s *Store) MarkAttempt(ctx context.Context, id int) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO comic_fetches
			(comic_id, status, attempts, last_attempt_at)
		VALUES ($1, 'pending', 1, now())
		ON CONFLICT (comic_id) DO UPDATE
		SET status = 'pending',
			attempts = comic_fetches.attempts + 1,
			last_attempt_at = now(),
			last_error = NULL
	`, id)

	return err
}

func (s *Store) MarkFailed(
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
				ELSE COALESCE(
					next_retry_at,
					now() + interval '5 minutes'
				)
			END
		WHERE comic_id = $1
	`, id, message)

	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("record failure: comic %d has no attempt row", id)
	}

	return nil
}

func (s *Store) SaveProgress(
	ctx context.Context,
	progress archive.Progress,
) error {
	_, err := s.pool.Exec(ctx, progressSQL,
		progress.LatestSeenID,
		progress.LatestSavedID,
		progress.NextID,
	)

	return err
}

const progressSQL = `
	UPDATE sync_state
	SET latest_seen_id = GREATEST(latest_seen_id, $1),
		latest_saved_id = GREATEST(latest_saved_id, $2),
		backfill_next_id = GREATEST(backfill_next_id, $3),
		updated_at = now()
	WHERE name = 'xkcd'
`

func (s *Store) SaveComplete(
	ctx context.Context,
	comic archive.Comic,
	image archive.Image,
	progress archive.Progress,
) error {
	return s.saveComplete(ctx, comic, image, &progress)
}

func (s *Store) saveComplete(
	ctx context.Context,
	comic archive.Comic,
	image archive.Image,
	progress *archive.Progress,
) error {
	if len(image.Data) == 0 {
		return fmt.Errorf("cannot save an empty image")
	}

	published, err := time.Parse(
		"2006-1-2",
		fmt.Sprintf("%s-%s-%s", comic.Year, comic.Month, comic.Day),
	)
	if err != nil {
		return fmt.Errorf("invalid comic publication date: %w", err)
	}

	metadata := comic.Metadata
	if len(metadata) == 0 {
		metadata, err = json.Marshal(comic)
		if err != nil {
			return err
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	if _, err := tx.Exec(ctx, `
		INSERT INTO comics (
			id, title, safe_title, alt, published_date,
			upstream_image_url, metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE
		SET title = EXCLUDED.title,
			safe_title = EXCLUDED.safe_title,
			alt = EXCLUDED.alt,
			published_date = EXCLUDED.published_date,
			upstream_image_url = EXCLUDED.upstream_image_url,
			metadata = EXCLUDED.metadata,
			metadata_fetched_at = now()
	`,
		comic.Num,
		comic.Title,
		comic.SafeTitle,
		comic.Alt,
		published,
		comic.Img,
		[]byte(metadata),
	); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO comic_images (
			comic_id, content_type, byte_size, sha256, data
		)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (comic_id) DO UPDATE
		SET content_type = EXCLUDED.content_type,
			byte_size = EXCLUDED.byte_size,
			sha256 = EXCLUDED.sha256,
			data = EXCLUDED.data,
			downloaded_at = now()
	`,
		comic.Num,
		image.ContentType,
		len(image.Data),
		image.SHA256,
		image.Data,
	); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO comic_fetches (comic_id, status)
		VALUES ($1, 'complete')
		ON CONFLICT (comic_id) DO UPDATE
		SET status = 'complete',
			last_error = NULL,
			next_retry_at = NULL
	`, comic.Num); err != nil {
		return err
	}

	if progress != nil {
		if _, err := tx.Exec(ctx, progressSQL,
			progress.LatestSeenID,
			progress.LatestSavedID,
			progress.NextID,
		); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE sync_state
		SET latest_saved_id = GREATEST(latest_saved_id, $1),
			updated_at = now()
		WHERE name = 'xkcd'
	`, comic.Num); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Store) SaveSkipped(
	ctx context.Context,
	id int,
	reason string,
	progress archive.Progress,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	if _, err := tx.Exec(ctx, `
		INSERT INTO comic_fetches (comic_id, status, last_error)
		VALUES ($1, 'unavailable', $2)
		ON CONFLICT (comic_id) DO UPDATE
		SET status = 'unavailable',
			last_error = EXCLUDED.last_error,
			next_retry_at = NULL
	`, id, reason); err != nil {
		return err
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
