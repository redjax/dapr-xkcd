package viewer

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

const galleryPageSize = 24

type GalleryComic struct {
	ID    int
	Title string
}

type GalleryPage struct {
	Comics []GalleryComic
	NextID int
}

type GallerySource interface {
	ListGallery(
		ctx context.Context,
		beforeID int,
		limit int,
	) (GalleryPage, error)
}

type GalleryHandler struct {
	source  GallerySource
	log     *slog.Logger
	timeout time.Duration
	tmpl    *template.Template
}

type galleryView struct {
	GalleryPage

	BeforeID int
	Message  string
}

func NewGallery(
	source GallerySource,
	log *slog.Logger,
	timeout time.Duration,
) (*GalleryHandler, error) {
	tmpl, err := template.New("gallery.html").
		ParseFS(templates, "templates/gallery.html")
	if err != nil {
		return nil, fmt.Errorf("parse gallery template: %w", err)
	}

	return &GalleryHandler{
		source:  source,
		log:     log,
		timeout: timeout,
		tmpl:    tmpl,
	}, nil
}

func (h *GalleryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	data := galleryView{}

	if r.URL.Query().Has("before") {
		id, err := strconv.Atoi(r.URL.Query().Get("before"))
		if err != nil || id < 1 {
			data.Message = "The before parameter must be a positive comic ID."
			h.render(w, r, http.StatusBadRequest, data)
			return
		}
		data.BeforeID = id
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	page, err := h.source.ListGallery(
		ctx,
		data.BeforeID,
		galleryPageSize,
	)
	if err != nil {
		h.log.Error("load comic gallery", "error", err)
		data.Message = "The comic archive is temporarily unavailable."
		h.render(w, r, http.StatusServiceUnavailable, data)
		return
	}

	data.GalleryPage = page
	h.render(w, r, http.StatusOK, data)
}

func (h *GalleryHandler) render(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	data galleryView,
) {
	var output bytes.Buffer

	if err := h.tmpl.ExecuteTemplate(&output, "gallery.html", data); err != nil {
		h.log.Error("render comic gallery", "error", err)
		http.Error(
			w,
			"Unable to render comic gallery",
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
