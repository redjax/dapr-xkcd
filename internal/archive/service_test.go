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
	failSave bool
	failed   int
	skipped  int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		progress: Progress{NextID: 1, LatestSeenID: 10},
		complete: make(map[int]bool),
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

func (s *fakeStore) ObserveLatest(_ context.Context, id int) error {
	if id > s.progress.LatestSeenID {
		s.progress.LatestSeenID = id
	}
	return nil
}

func (s *fakeStore) MarkAttempt(context.Context, int) error {
	return nil
}

func (s *fakeStore) MarkFailed(_ context.Context, id int, _ string) error {
	s.failed = id
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
	s.progress = progress
	return nil
}

func (s *fakeStore) SaveSkipped(
	_ context.Context,
	id int,
	_ string,
	progress Progress,
) error {
	s.skipped = id
	s.progress = progress
	return nil
}

func (s *fakeStore) SaveProgress(_ context.Context, progress Progress) error {
	s.progress = progress
	return nil
}

type fakeSource struct {
	calls    int
	imageErr error
}

func (s *fakeSource) Latest(context.Context) (Comic, error) {
	s.calls++
	return Comic{Num: 10, Img: "https://imgs.xkcd.com/test.png"}, nil
}

func (s *fakeSource) ByID(_ context.Context, id int) (Comic, error) {
	s.calls++
	return Comic{Num: id, Img: "https://imgs.xkcd.com/test.png"}, nil
}

func (s *fakeSource) DownloadImage(context.Context, string) (Image, error) {
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

func TestBackfillFailureKeepsCursor(t *testing.T) {
	for _, failImage := range []bool{false, true} {
		store := newFakeStore()
		source := &fakeSource{}
		if failImage {
			source.imageErr = errors.New("image failed")
		} else {
			store.failSave = true
		}

		service := testService(source, store)
		if err := service.BackfillOne(context.Background()); err == nil {
			t.Fatal("expected failure")
		}
		if store.progress.NextID != 1 || store.complete[1] {
			t.Fatal("failure must not commit content or progress")
		}
		if store.failed != 1 {
			t.Fatal("expected failure record")
		}
	}
}

func TestBackfillAlreadyComplete(t *testing.T) {
	store := newFakeStore()
	store.complete[1] = true
	source := &fakeSource{}

	if err := testService(source, store).BackfillOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if source.calls != 0 || store.progress.NextID != 2 {
		t.Fatal("expected cursor advancement without upstream requests")
	}
}

func TestBackfillSkips404(t *testing.T) {
	store := newFakeStore()
	store.progress = Progress{NextID: 404, LatestSeenID: 500}
	source := &fakeSource{}

	if err := testService(source, store).BackfillOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if source.calls != 0 || store.skipped != 404 || store.progress.NextID != 405 {
		t.Fatal("expected atomic skip and cursor advancement")
	}
}

func TestLatestImageFailureKeepsSavedMarker(t *testing.T) {
	store := newFakeStore()
	store.progress.LatestSeenID = 0
	source := &fakeSource{imageErr: errors.New("image failed")}

	if err := testService(source, store).CheckLatest(context.Background()); err == nil {
		t.Fatal("expected failure")
	}
	if store.progress.LatestSeenID != 10 || store.progress.LatestSavedID != 0 {
		t.Fatal("observed marker may advance, saved marker must not")
	}
}
