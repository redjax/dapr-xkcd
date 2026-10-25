package httpapi

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/redjax/dapr-xkcd/internal/archive"
)

//go:embed landing.html
var landingHTML []byte

//go:embed all:static
var staticFiles embed.FS

type Service interface {
	CheckLatest(ctx context.Context) error
	BackfillOne(ctx context.Context) error
	GetComic(ctx context.Context, id int) (archive.Comic, error)
	GetImage(ctx context.Context, id int) (archive.Image, error)
	LatestSaved(ctx context.Context) (archive.Comic, error)
	Status(ctx context.Context) (archive.Progress, error)
}

type retryService interface {
	RetryOne(ctx context.Context) error
}

type Options struct {
	Address          string
	IngestionTimeout time.Duration
	StateReadTimeout time.Duration
	Dashboard        http.Handler
	Viewer           http.Handler
	Gallery          http.Handler
}

type API struct {
	service Service
	log     *slog.Logger
	options Options
}

func NewServer(
	service Service,
	log *slog.Logger,
	options Options,
) *http.Server {
	api := &API{
		service: service,
		log:     log,
		options: options,
	}

	mux := http.NewServeMux()

	staticRoot, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(fmt.Errorf("create embedded static filesystem: %w", err))
	}

	mux.Handle(
		"GET /static/",
		http.StripPrefix("/static/", http.FileServer(http.FS(staticRoot))),
	)

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)

		if r.Method != http.MethodHead {
			_, _ = w.Write(landingHTML)
		}
	})

	mux.HandleFunc("/latest", api.job(service.CheckLatest))
	mux.HandleFunc("/backfill", api.job(service.BackfillOne))

	if retries, ok := service.(retryService); ok {
		mux.HandleFunc("/retry", api.job(retries.RetryOne))
	}

	mux.HandleFunc("GET /comics/{id}", api.serveComic)
	mux.HandleFunc("GET /comics/{id}/image", api.serveImage)
	mux.HandleFunc("GET /sync/status", api.serveStatus)

	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	if options.Viewer != nil {
		mux.Handle("GET /comics", options.Viewer)

		mux.HandleFunc("GET /comics/{$}", func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			target := "/comics"
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}

			http.Redirect(
				w,
				r,
				target,
				http.StatusTemporaryRedirect,
			)
		})
	}

	if options.Dashboard != nil {
		mux.Handle("GET /dashboard", options.Dashboard)
	}

	if options.Gallery != nil {
		mux.Handle("GET /comics/all", options.Gallery)
	}

	return &http.Server{
		Addr:              options.Address,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout: options.IngestionTimeout +
			options.StateReadTimeout +
			10*time.Second,
		IdleTimeout: 60 * time.Second,
	}
}
