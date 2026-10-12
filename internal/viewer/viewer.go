package viewer

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/redjax/dapr-xkcd/internal/archive"
)

//go:embed templates/viewer.html templates/gallery.html
var templates embed.FS

type ComicView struct {
	Comic archive.Comic

	PreviousID int
	NextID     int
	FirstID    int
	LastID     int

	AvailableCount int64
}

type Source interface {
	// An ID of zero selects the highest fully archived comic.
	LoadComicView(ctx context.Context, id int) (ComicView, error)

	RandomComicID(ctx context.Context) (int, error)
}

type Handler struct {
	source  Source
	log     *slog.Logger
	timeout time.Duration
	tmpl    *template.Template
}

type page struct {
	ComicView

	HasComic bool
	Message  string
}

func New(
	source Source,
	log *slog.Logger,
	timeout time.Duration,
) (*Handler, error) {
	tmpl, err := template.New("viewer.html").
		Funcs(template.FuncMap{
			"published": publicationDate,
		}).
		ParseFS(templates, "templates/viewer.html")
	if err != nil {
		return nil, fmt.Errorf("parse viewer template: %w", err)
	}

	return &Handler{
		source:  source,
		log:     log,
		timeout: timeout,
		tmpl:    tmpl,
	}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	query := r.URL.Query()

	if query.Has("random") {
		if query.Get("random") != "1" {
			h.render(w, r, http.StatusBadRequest, page{
				Message: "The random parameter must be 1.",
			})
			return
		}

		id, err := h.source.RandomComicID(ctx)
		if errors.Is(err, archive.ErrNotFound) {
			h.render(w, r, http.StatusOK, page{
				Message: "No comics have been fully archived yet.",
			})
			return
		}
		if err != nil {
			h.databaseError(w, r, err)
			return
		}

		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(
			w,
			r,
			fmt.Sprintf("/comics?id=%d", id),
			http.StatusSeeOther,
		)
		return
	}

	id := 0
	explicitID := query.Has("id")

	if explicitID {
		var err error
		id, err = strconv.Atoi(query.Get("id"))
		if err != nil || id < 1 {
			h.render(w, r, http.StatusBadRequest, page{
				Message: "Comic ID must be a positive integer.",
			})
			return
		}
	}

	comicView, err := h.source.LoadComicView(ctx, id)
	if errors.Is(err, archive.ErrNotFound) {
		if explicitID {
			h.render(w, r, http.StatusNotFound, page{
				Message: fmt.Sprintf(
					"Comic %d has not been fully archived.",
					id,
				),
			})
		} else {
			h.render(w, r, http.StatusOK, page{
				Message: "No comics have been fully archived yet.",
			})
		}
		return
	}
	if err != nil {
		h.databaseError(w, r, err)
		return
	}

	h.render(w, r, http.StatusOK, page{
		ComicView: comicView,
		HasComic:  true,
	})
}

func (h *Handler) databaseError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	h.log.Error("load comic viewer", "error", err)

	h.render(w, r, http.StatusServiceUnavailable, page{
		Message: "The comic archive is temporarily unavailable. Please try again.",
	})
}

func (h *Handler) render(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	data page,
) {
	var output bytes.Buffer

	if err := h.tmpl.ExecuteTemplate(&output, "viewer.html", data); err != nil {
		h.log.Error("render comic viewer", "error", err)
		http.Error(
			w,
			"Unable to render comic viewer",
			http.StatusInternalServerError,
		)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)

	if r.Method != http.MethodHead {
		_, _ = w.Write(output.Bytes())
	}
}

func publicationDate(comic archive.Comic) string {
	value := fmt.Sprintf(
		"%s-%s-%s",
		comic.Year,
		comic.Month,
		comic.Day,
	)

	date, err := time.Parse("2006-1-2", value)
	if err != nil {
		return value
	}

	return date.Format("January 2, 2006")
}
