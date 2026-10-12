package postgres

import (
	"context"

	"github.com/redjax/dapr-xkcd/internal/dashboard"
)

var _ dashboard.Source = (*Store)(nil)

func (s *Store) DashboardStats(
	ctx context.Context,
) (dashboard.Stats, error) {
	var stats dashboard.Stats

	err := s.pool.QueryRow(ctx, `
		SELECT
		    (
		        SELECT count(*)
		        FROM comic_fetches f
		        JOIN comics c ON c.id = f.comic_id
		        JOIN comic_images i ON i.comic_id = c.id
		        WHERE f.status = 'complete'
		    ),
		    (
		        SELECT count(*)
		        FROM comic_fetches
		        WHERE status = 'pending'
		    ),
		    (
		        SELECT count(*)
		        FROM comic_fetches
		        WHERE status = 'retry_pending'
		    ),
		    (
		        SELECT count(*)
		        FROM comic_fetches
		        WHERE status = 'unavailable'
		    ),
		    (
		        SELECT COALESCE(sum(byte_size), 0)::bigint
		        FROM comic_images
		    ),
		    latest_seen_id,
		    latest_saved_id,
		    backfill_next_id,
		    updated_at
		FROM sync_state
		WHERE name = 'xkcd'
	`).Scan(
		&stats.CompleteCount,
		&stats.PendingCount,
		&stats.RetryCount,
		&stats.UnavailableCount,
		&stats.ImageBytes,
		&stats.LatestSeenID,
		&stats.LatestSavedID,
		&stats.NextID,
		&stats.ProgressAt,
	)
	if err != nil {
		return stats, err
	}

	recent, err := s.pool.Query(ctx, `
		SELECT c.id, c.title, i.downloaded_at
		FROM comics c
		JOIN comic_images i ON i.comic_id = c.id
		JOIN comic_fetches f ON f.comic_id = c.id
		WHERE f.status = 'complete'
		ORDER BY i.downloaded_at DESC, c.id DESC
		LIMIT 10
	`)
	if err != nil {
		return stats, err
	}

	for recent.Next() {
		var comic dashboard.RecentComic
		if err := recent.Scan(
			&comic.ID,
			&comic.Title,
			&comic.DownloadedAt,
		); err != nil {
			recent.Close()
			return stats, err
		}
		stats.RecentComics = append(stats.RecentComics, comic)
	}

	err = recent.Err()
	recent.Close()
	if err != nil {
		return stats, err
	}

	failures, err := s.pool.Query(ctx, `
		SELECT
		    comic_id,
		    attempts,
		    last_attempt_at,
		    COALESCE(last_error, '')
		FROM comic_fetches
		WHERE status = 'retry_pending'
		  AND last_attempt_at IS NOT NULL
		ORDER BY last_attempt_at DESC, comic_id DESC
		LIMIT 10
	`)
	if err != nil {
		return stats, err
	}
	defer failures.Close()

	for failures.Next() {
		var failure dashboard.Failure
		if err := failures.Scan(
			&failure.ComicID,
			&failure.Attempts,
			&failure.LastAttemptAt,
			&failure.Message,
		); err != nil {
			return stats, err
		}
		stats.Failures = append(stats.Failures, failure)
	}

	return stats, failures.Err()
}
