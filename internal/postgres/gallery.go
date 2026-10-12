package postgres

import (
	"context"
	"fmt"

	"github.com/redjax/dapr-xkcd/internal/viewer"
)

var _ viewer.GallerySource = (*Store)(nil)

func (s *Store) ListGallery(
	ctx context.Context,
	beforeID int,
	limit int,
) (viewer.GalleryPage, error) {
	var page viewer.GalleryPage

	if beforeID < 0 || limit < 1 || limit > 100 {
		return page, fmt.Errorf("invalid gallery pagination parameters")
	}

	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.title
		FROM comics c
		JOIN comic_images i ON i.comic_id = c.id
		JOIN comic_fetches f ON f.comic_id = c.id
		WHERE f.status = 'complete'
		  AND ($1::integer = 0 OR c.id < $1)
		ORDER BY c.id DESC
		LIMIT $2
	`, beforeID, limit+1)
	if err != nil {
		return page, fmt.Errorf("list gallery comics: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var comic viewer.GalleryComic
		if err := rows.Scan(&comic.ID, &comic.Title); err != nil {
			return page, err
		}
		page.Comics = append(page.Comics, comic)
	}

	if err := rows.Err(); err != nil {
		return page, err
	}

	if len(page.Comics) > limit {
		page.Comics = page.Comics[:limit]
		page.NextID = page.Comics[len(page.Comics)-1].ID
	}

	return page, nil
}
