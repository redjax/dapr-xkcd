package archive

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

type fakeStore struct {
	progress Progress
	complete map[int]bool
	managed  map[int]bool

	failSave  bool
	failQueue bool

	failed  int
	skipped int

	retryID          int
	retryFailed      int
	retrySaved       int
	markAttemptCalls int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		progress: Progress{
			NextID:       1,
			LatestSeenID: 10,
		},
		complete: make(map[int]bool),
		managed:  make(map[int]bool),
	}
}

func (s *fakeStore) GetComic(context.Context, int) (Comic, error) {
	return Comic{}, ErrNotFound
}

func (s *fakeStore) GetImage(context.Context, int) (Image, error) {
	return Image{}, ErrNotFound
}

func (s *fakeStore) LoadProgress(context.Context) (Progress, error) {
	return s.progress, nil
}

func (s *fakeStore) IsComplete(_ context.Context, id int) (bool, error) {
	return s.complete[id], nil
}

func (s *fakeStore) RetryManaged(_ context.Context, id int) (bool, error) {
	return s.managed[id], nil
}

func (s *fakeStore) ObserveLatest(_ context.Context, id int) error {
	if id > s.progress.LatestSeenID {
		s.progress.LatestSeenID = id
	}
	return nil
}

func (s *fakeStore) MarkAttempt(context.Context, int) error {
	s.markAttemptCalls++
	return nil
}

func (s *fakeStore) MarkFailed(_ context.Context, id int, _ string) error {
	s.failed = id
	s.managed[id] = true
	return nil
}

func (s *fakeStore) SaveComplete(
	_ context.Context,
	comic Comic,
	_ Image,
	progress Progress,
) error {
	if s.failSave {
		return errors.New("transaction failed")
	}

	s.complete[comic.Num] = true
	s.managed[comic.Num] = false
	s.progress = progress

	if comic.Num > s.progress.LatestSavedID {
		s.progress.LatestSavedID = comic.Num
	}

	return nil
}

func (s *fakeStore) SaveRetryComplete(
	_ context.Context,
	comic Comic,
	_ Image,
) error {
	if s.failSave {
		return errors.New("transaction failed")
	}

	s.complete[comic.Num] = true
	s.managed[comic.Num] = false
	s.retrySaved = comic.Num

	if comic.Num > s.progress.LatestSavedID {
		s.progress.LatestSavedID = comic.Num
	}

	return nil
}

func (s *fakeStore) SaveSkipped(
	_ context.Context,
	id int,
	_ string,
	progress Progress,
) error {
	s.skipped = id
	s.managed[id] = true
	s.progress = progress
	return nil
}

func (s *fakeStore) SaveProgress(
	_ context.Context,
	progress Progress,
) error {
	s.progress = progress
	return nil
}

func (s *fakeStore) QueueBackfillFailure(
	_ context.Context,
	id int,
	_ string,
	progress Progress,
) error {
	if s.failQueue {
		return errors.New("queue transaction failed")
	}

	s.failed = id
	s.managed[id] = true
	s.progress = progress
	return nil
}

func (s *fakeStore) ClaimRetry(context.Context) (int, error) {
	id := s.retryID
	s.retryID = 0
	return id, nil
}

func (s *fakeStore) FinishRetryFailure(
	_ context.Context,
	id int,
	_ string,
) error {
	s.retryFailed = id
	s.managed[id] = true
	return nil
}

type fakeSource struct {
	calls    int
	imageErr error
}

func (s *fakeSource) Latest(context.Context) (Comic, error) {
	s.calls++
	return Comic{
		Num: 10,
		Img: "https://imgs.xkcd.com/test.png",
	}, nil
}

func (s *fakeSource) ByID(_ context.Context, id int) (Comic, error) {
	s.calls++
	return Comic{
		Num: id,
		Img: "https://imgs.xkcd.com/test.png",
	}, nil
}

func (s *fakeSource) DownloadImage(
	context.Context,
	string,
) (Image, error) {
	s.calls++
	return Image{Data: []byte("test")}, s.imageErr
}

func testService(source ComicSource, store Store) *Service {
	return NewService(
		source,
		store,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
}

func TestBackfillSuccess(t *testing.T) {
	store := newFakeStore()
	service := testService(&fakeSource{}, store)

	if err := service.BackfillOne(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !store.complete[1] || store.progress.NextID != 2 {
		t.Fatal("expected complete comic 1 and cursor 2")
	}
}

func TestBackfillFailureQueuesAndAdvances(t *testing.T) {
	for _, failImage := range []bool{false, true} {
		name := "save failure"
		if failImage {
			name = "image failure"
		}

		t.Run(name, func(t *testing.T) {
			store := newFakeStore()
			source := &fakeSource{}

			if failImage {
				source.imageErr = errors.New("image failed")
			} else {
				store.failSave = true
			}

			if err := testService(source, store).BackfillOne(
				context.Background(),
			); err != nil {
				t.Fatalf("queued failure returned error: %v", err)
			}

			if store.progress.NextID != 2 {
				t.Fatal("expected cursor 2 after queuing failure")
			}
			if store.complete[1] {
				t.Fatal("failed comic must not be complete")
			}
			if store.failed != 1 || !store.managed[1] {
				t.Fatal("expected comic 1 in retry queue")
			}
		})
	}
}

func TestBackfillFailurePersistenceKeepsCursor(t *testing.T) {
	store := newFakeStore()
	store.failQueue = true

	source := &fakeSource{
		imageErr: errors.New("image failed"),
	}

	err := testService(source, store).BackfillOne(context.Background())
	if err == nil {
		t.Fatal("expected failure when queue transaction fails")
	}

	if store.progress.NextID != 1 {
		t.Fatal("cursor must not advance without a durable failure record")
	}
}

func TestBackfillAlreadyComplete(t *testing.T) {
	store := newFakeStore()
	store.complete[1] = true
	source := &fakeSource{}

	if err := testService(source, store).BackfillOne(
		context.Background(),
	); err != nil {
		t.Fatal(err)
	}

	if source.calls != 0 || store.progress.NextID != 2 {
		t.Fatal("expected advancement without upstream requests")
	}
}

func TestBackfillAlreadyQueued(t *testing.T) {
	store := newFakeStore()
	store.managed[1] = true
	source := &fakeSource{}

	if err := testService(source, store).BackfillOne(
		context.Background(),
	); err != nil {
		t.Fatal(err)
	}

	if source.calls != 0 || store.progress.NextID != 2 {
		t.Fatal("expected queued comic to be bypassed without fetching")
	}
	if store.markAttemptCalls != 0 {
		t.Fatal("must not restart queued comic's normal attempt")
	}
}

func TestBackfillSkips404(t *testing.T) {
	store := newFakeStore()
	store.progress = Progress{
		NextID:       404,
		LatestSeenID: 500,
	}
	source := &fakeSource{}

	if err := testService(source, store).BackfillOne(
		context.Background(),
	); err != nil {
		t.Fatal(err)
	}

	if source.calls != 0 ||
		store.skipped != 404 ||
		store.progress.NextID != 405 {
		t.Fatal("expected atomic skip and cursor advancement")
	}
}

func TestLatestImageFailureKeepsSavedMarker(t *testing.T) {
	store := newFakeStore()
	store.progress.LatestSeenID = 0

	source := &fakeSource{
		imageErr: errors.New("image failed"),
	}

	if err := testService(source, store).CheckLatest(
		context.Background(),
	); err == nil {
		t.Fatal("expected latest ingestion failure")
	}

	if store.progress.LatestSeenID != 10 ||
		store.progress.LatestSavedID != 0 {
		t.Fatal("observed marker may advance; saved marker must not")
	}
	if !store.managed[10] {
		t.Fatal("latest failure must enter retry queue")
	}
}

func TestLatestDoesNotBypassRetryQueue(t *testing.T) {
	store := newFakeStore()
	store.managed[10] = true
	source := &fakeSource{}

	if err := testService(source, store).CheckLatest(
		context.Background(),
	); err != nil {
		t.Fatal(err)
	}

	// Only the latest metadata request should occur.
	if source.calls != 1 {
		t.Fatalf("upstream calls = %d, want 1", source.calls)
	}
	if store.markAttemptCalls != 0 {
		t.Fatal("latest check must not restart a queued or exhausted comic")
	}
}

func TestRetrySuccessKeepsBackfillCursor(t *testing.T) {
	store := newFakeStore()
	store.progress.NextID = 1800
	store.progress.LatestSeenID = 2000
	store.retryID = 1608
	store.managed[1608] = true

	if err := testService(&fakeSource{}, store).RetryOne(
		context.Background(),
	); err != nil {
		t.Fatal(err)
	}

	if store.retrySaved != 1608 || !store.complete[1608] {
		t.Fatal("expected retry to save comic 1608")
	}
	if store.progress.NextID != 1800 {
		t.Fatal("retry success must not change backfill cursor")
	}
}

func TestRetryFailureKeepsBackfillCursor(t *testing.T) {
	store := newFakeStore()
	store.progress.NextID = 1800
	store.retryID = 1608

	source := &fakeSource{
		imageErr: errors.New("HTTP 403"),
	}

	if err := testService(source, store).RetryOne(
		context.Background(),
	); err != nil {
		t.Fatal(err)
	}

	if store.retryFailed != 1608 {
		t.Fatal("expected retry failure to be recorded")
	}
	if store.progress.NextID != 1800 {
		t.Fatal("retry failure must not change backfill cursor")
	}
}

func TestRetryNoWork(t *testing.T) {
	store := newFakeStore()
	source := &fakeSource{}

	if err := testService(source, store).RetryOne(
		context.Background(),
	); err != nil {
		t.Fatal(err)
	}

	if source.calls != 0 {
		t.Fatal("empty retry queue must not make upstream requests")
	}
}
