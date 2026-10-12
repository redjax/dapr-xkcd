package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/redjax/dapr-xkcd/internal/archive"
)

type fakeService struct {
	readErr error
}

func (f *fakeService) CheckLatest(context.Context) error {
	return nil
}

func (f *fakeService) BackfillOne(context.Context) error {
	return nil
}

func (f *fakeService) GetComic(_ context.Context, id int) (archive.Comic, error) {
	return archive.Comic{Num: id}, f.readErr
}

func (f *fakeService) GetImage(context.Context, int) (archive.Image, error) {
	return archive.Image{
		Data:        []byte("test-image"),
		ContentType: "image/png",
		SHA256:      "test",
	}, f.readErr
}

func (f *fakeService) LatestSaved(context.Context) (archive.Comic, error) {
	return archive.Comic{Num: 10}, f.readErr
}

func (f *fakeService) Status(context.Context) (archive.Progress, error) {
	return archive.Progress{NextID: 1}, f.readErr
}

func TestRoutes(t *testing.T) {
	tests := []struct {
		method string
		path   string
		err    error
		want   int
	}{
		{"GET", "/health/live", nil, 204},
		{"OPTIONS", "/latest", nil, 204},
		{"POST", "/latest", nil, 204},
		{"POST", "/backfill", nil, 204},
		{"GET", "/latest", nil, 405},
		{"GET", "/comics/1", nil, 200},
		{"GET", "/comics/latest", nil, 200},
		{"GET", "/comics/1/image", nil, 200},
		{"GET", "/comics/nope/image", nil, 400},
		{"GET", "/comics/1/image", archive.ErrNotFound, 404},
		{"GET", "/sync/status", nil, 200},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			server := NewServer(
				&fakeService{readErr: tt.err},
				slog.New(slog.NewTextHandler(io.Discard, nil)),
				Options{
					IngestionTimeout: time.Second,
					StateReadTimeout: time.Second,
				},
			)

			response := httptest.NewRecorder()
			request := httptest.NewRequest(tt.method, tt.path, nil)
			server.Handler.ServeHTTP(response, request)

			if response.Code != tt.want {
				t.Fatalf("status = %d, want %d", response.Code, tt.want)
			}
			if tt.path == "/comics/1/image" && tt.want == 200 {
				if response.Header().Get("Content-Type") != "image/png" {
					t.Fatal("expected image content type")
				}
				if response.Body.String() != "test-image" {
					t.Fatal("expected stored image bytes")
				}
			}
		})
	}
}
