package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/redjax/dapr-xkcd/internal/archive"
	"github.com/redjax/dapr-xkcd/internal/viewer"
)

var _ viewer.Source = (*Store)(nil)

func (s *Store) LoadComicView(
	ctx context.Context,
	id int,
) (viewer.ComicView, error) {
	var result viewer.ComicView
	var metadata []byte

	var err error

	if id == 0 {
		// Default to the highest fully archived comic number.
		err = s.pool.QueryRow(ctx, `
			SELECT c.metadata
			FROM comics c
			JOIN comic_images i ON i.comic_id = c.id
			JOIN comic_fetches f ON f.comic_id = c.id
			WHERE f.status = 'complete'
			ORDER BY c.id DESC
			LIMIT 1
		`).Scan(&metadata)
	} else {
		err = s.pool.QueryRow(ctx, `
			SELECT c.metadata
			FROM comics c
			JOIN comic_images i ON i.comic_id = c.id
			JOIN comic_fetches f ON f.comic_id = c.id
			WHERE f.status = 'complete'
			  AND c.id = $1
		`, id).Scan(&metadata)
	}

	if errors.Is(err, pgx.ErrNoRows) {
		return result, archive.ErrNotFound
	}
	if err != nil {
		return result, fmt.Errorf("load viewer comic: %w", err)
	}

	if err := json.Unmarshal(metadata, &result.Comic); err != nil {
		return result, fmt.Errorf("decode viewer comic: %w", err)
	}

	result.Comic.Metadata = json.RawMessage(metadata)

	err = s.pool.QueryRow(ctx, `
		SELECT
		    COALESCE(
		        max(c.id) FILTER (WHERE c.id < $1),
		        0
		    ),
		    COALESCE(
		        min(c.id) FILTER (WHERE c.id > $1),
		        0
		    ),
		    COALESCE(min(c.id), 0),
		    COALESCE(max(c.id), 0),
		    count(*)
		FROM comics c
		JOIN comic_images i ON i.comic_id = c.id
		JOIN comic_fetches f ON f.comic_id = c.id
		WHERE f.status = 'complete'
	`, result.Comic.Num).Scan(
		&result.PreviousID,
		&result.NextID,
		&result.FirstID,
		&result.LastID,
		&result.AvailableCount,
	)
	if err != nil {
		return result, fmt.Errorf("load viewer navigation: %w", err)
	}

	return result, nil
}

func (s *Store) RandomComicID(ctx context.Context) (int, error) {
	var id int

	err := s.pool.QueryRow(ctx, `
		SELECT c.id
		FROM comics c
		JOIN comic_images i ON i.comic_id = c.id
		JOIN comic_fetches f ON f.comic_id = c.id
		WHERE f.status = 'complete'
		ORDER BY random()
		LIMIT 1
	`).Scan(&id)

	if errors.Is(err, pgx.ErrNoRows) {
		return 0, archive.ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("select random comic: %w", err)
	}

	return id, nil
}
