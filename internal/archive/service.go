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

	if complete {
		if comic.Num > progress.LatestSavedID {
			progress.LatestSavedID = comic.Num
		}
		return s.store.SaveProgress(ctx, progress)
	}

	managed, err := s.store.RetryManaged(ctx, comic.Num)
	if err != nil {
		return err
	}
	if managed {
		return nil
	}

	if err := s.store.MarkAttempt(ctx, comic.Num); err != nil {
		return err
	}

	image, err := s.downloadComicImage(ctx, comic)
	if err != nil {
		return s.recordFailure(ctx, comic.Num, err)
	}

	if comic.Num > progress.LatestSavedID {
		progress.LatestSavedID = comic.Num
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

	// An earlier latest check or deployment may already have queued
	// this comic. Do not restart its retry budget.
	managed, err := s.store.RetryManaged(ctx, id)
	if err != nil {
		return err
	}
	if managed {
		return s.store.SaveProgress(ctx, progress)
	}

	if err := s.store.MarkAttempt(ctx, id); err != nil {
		return err
	}

	comic, err := s.source.ByID(ctx, id)
	if err != nil {
		return s.queueBackfillFailure(ctx, id, err, progress)
	}

	if comic.Num != id {
		return s.queueBackfillFailure(ctx, id, fmt.Errorf(
			"requested comic %d, received %d",
			id,
			comic.Num,
		), progress)
	}

	image, err := s.downloadComicImage(ctx, comic)
	if err != nil {
		return s.queueBackfillFailure(ctx, id, err, progress)
	}

	if err := s.store.SaveComplete(ctx, comic, image, progress); err != nil {
		return s.queueBackfillFailure(ctx, id, err, progress)
	}

	s.log.Info("archived backfill comic", "id", id)
	return nil
}

func (s *Service) RetryOne(ctx context.Context) error {
	if !s.mu.TryLock() {
		return ErrBusy
	}
	defer s.mu.Unlock()

	id, err := s.store.ClaimRetry(ctx)
	if err != nil {
		return fmt.Errorf("claim comic retry: %w", err)
	}
	if id == 0 {
		return nil
	}

	comic, err := s.source.ByID(ctx, id)
	if err == nil && comic.Num != id {
		err = fmt.Errorf(
			"requested comic %d, received %d",
			id,
			comic.Num,
		)
	}

	if err == nil {
		var image Image
		image, err = s.downloadComicImage(ctx, comic)
		if err == nil {
			err = s.store.SaveRetryComplete(ctx, comic, image)
		}
	}

	if err == nil {
		s.log.Info("archived retried comic", "id", id)
		return nil
	}

	failureCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		5*time.Second,
	)
	defer cancel()

	if saveErr := s.store.FinishRetryFailure(
		failureCtx,
		id,
		err.Error(),
	); saveErr != nil {
		return errors.Join(
			err,
			fmt.Errorf("record retry failure: %w", saveErr),
		)
	}

	s.log.Warn("comic retry failed", "id", id, "error", err)
	return nil
}

func (s *Service) downloadComicImage(
	ctx context.Context,
	comic Comic,
) (Image, error) {
	imageURL, err := ResolveImageURL(comic.Num, comic.Img)
	if err != nil {
		return Image{}, err
	}

	return s.source.DownloadImage(ctx, imageURL)
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

func (s *Service) queueBackfillFailure(
	ctx context.Context,
	id int,
	cause error,
	progress Progress,
) error {
	failureCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		5*time.Second,
	)
	defer cancel()

	if err := s.store.QueueBackfillFailure(
		failureCtx,
		id,
		cause.Error(),
		progress,
	); err != nil {
		return errors.Join(
			cause,
			fmt.Errorf("queue failure and advance backfill: %w", err),
		)
	}

	s.log.Warn(
		"backfill failure queued; continuing",
		"id", id,
		"error", cause,
	)

	return nil
}

func (s *Service) recordFailure(
	ctx context.Context,
	id int,
	cause error,
) error {
	failureCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		5*time.Second,
	)
	defer cancel()

	if err := s.store.MarkFailed(
		failureCtx,
		id,
		cause.Error(),
	); err != nil {
		return errors.Join(
			cause,
			fmt.Errorf("record failure: %w", err),
		)
	}

	return cause
}
