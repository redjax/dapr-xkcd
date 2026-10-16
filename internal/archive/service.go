package archive

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type Service struct {
	source ComicSource
	store  Store
	log    *slog.Logger
	mu     sync.Mutex
}

func NewService(source ComicSource, store Store, log *slog.Logger) *Service {
	return &Service{source: source, store: store, log: log}
}

func (s *Service) CheckLatest(ctx context.Context) error {
	if !s.mu.TryLock() {
		return ErrBusy
	}
	defer s.mu.Unlock()

	return s.checkLatest(ctx)
}

func (s *Service) checkLatest(ctx context.Context) error {
	comic, err := s.source.Latest(ctx)
	if err != nil {
		return err
	}

	if err := s.store.ObserveLatest(ctx, comic.Num); err != nil {
		return err
	}

	progress, err := s.store.LoadProgress(ctx)
	if err != nil {
		return err
	}

	complete, err := s.store.IsComplete(ctx, comic.Num)
	if err != nil {
		return err
	}

	if comic.Num > progress.LatestSavedID {
		progress.LatestSavedID = comic.Num
	}

	if complete {
		return s.store.SaveProgress(ctx, progress)
	}

	if err := s.store.MarkAttempt(ctx, comic.Num); err != nil {
		return err
	}

	imageURL, err := ResolveImageURL(comic.Num, comic.Img)
	if err != nil {
		return s.recordFailure(ctx, comic.Num, err)
	}

	image, err := s.source.DownloadImage(ctx, imageURL)
	if err != nil {
		return s.recordFailure(ctx, comic.Num, err)
	}

	if err := s.store.SaveComplete(ctx, comic, image, progress); err != nil {
		return s.recordFailure(ctx, comic.Num, err)
	}

	s.log.Info("archived latest comic", "id", comic.Num)
	return nil
}

func (s *Service) BackfillOne(ctx context.Context) error {
	if !s.mu.TryLock() {
		return ErrBusy
	}
	defer s.mu.Unlock()

	progress, err := s.store.LoadProgress(ctx)
	if err != nil {
		return err
	}

	if progress.LatestSeenID == 0 {
		return s.checkLatest(ctx)
	}

	if progress.NextID > progress.LatestSeenID {
		return nil
	}

	id := progress.NextID

	complete, err := s.store.IsComplete(ctx, id)
	if err != nil {
		return err
	}

	progress.NextID++

	if complete {
		return s.store.SaveProgress(ctx, progress)
	}

	if id == 404 {
		return s.store.SaveSkipped(
			ctx,
			id,
			"known missing XKCD comic",
			progress,
		)
	}

	if err := s.store.MarkAttempt(ctx, id); err != nil {
		return err
	}

	comic, err := s.source.ByID(ctx, id)
	if err != nil {
		return s.recordFailure(ctx, id, err)
	}
	if comic.Num != id {
		return s.recordFailure(ctx, id, fmt.Errorf(
			"requested comic %d, received %d", id, comic.Num,
		))
	}

	imageURL, err := ResolveImageURL(id, comic.Img)
	if err != nil {
		return s.recordFailure(ctx, id, err)
	}

	image, err := s.source.DownloadImage(ctx, imageURL)
	if err != nil {
		return s.recordFailure(ctx, id, err)
	}

	if err := s.store.SaveComplete(ctx, comic, image, progress); err != nil {
		return s.recordFailure(ctx, id, err)
	}

	s.log.Info("archived backfill comic", "id", id)
	return nil
}

func (s *Service) GetComic(ctx context.Context, id int) (Comic, error) {
	return s.store.GetComic(ctx, id)
}

func (s *Service) GetImage(ctx context.Context, id int) (Image, error) {
	return s.store.GetImage(ctx, id)
}

func (s *Service) LatestSaved(ctx context.Context) (Comic, error) {
	progress, err := s.store.LoadProgress(ctx)
	if err != nil {
		return Comic{}, err
	}
	if progress.LatestSavedID == 0 {
		return Comic{}, ErrNotFound
	}
	return s.store.GetComic(ctx, progress.LatestSavedID)
}

func (s *Service) Status(ctx context.Context) (Progress, error) {
	return s.store.LoadProgress(ctx)
}

func (s *Service) recordFailure(
	ctx context.Context,
	id int,
	cause error,
) error {
	// Record the outcome even if the request deadline already expired.
	failureCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		5*time.Second,
	)
	defer cancel()

	if err := s.store.MarkFailed(failureCtx, id, cause.Error()); err != nil {
		return errors.Join(cause, fmt.Errorf("record failure: %w", err))
	}
	return cause
}
