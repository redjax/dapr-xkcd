package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/redjax/dapr-xkcd/internal/archive"
)

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (a *API) job(run func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST, OPTIONS")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		ctx, cancel := context.WithTimeout(
			r.Context(),
			a.options.IngestionTimeout,
		)
		defer cancel()

		err := run(ctx)
		if errors.Is(err, archive.ErrBusy) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if err != nil {
			a.log.Error("ingestion failed", "route", r.URL.Path, "error", err)
			writeJSON(w, 503, map[string]string{
				"error": "ingestion failed; see application logs",
			})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *API) serveComic(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), a.options.StateReadTimeout)
	defer cancel()

	var comic archive.Comic
	var err error

	if r.PathValue("id") == "latest" {
		comic, err = a.service.LatestSaved(ctx)
	} else {
		id, parseErr := strconv.Atoi(r.PathValue("id"))
		if parseErr != nil || id < 1 {
			writeJSON(w, 400, map[string]string{"error": "invalid comic ID"})
			return
		}
		comic, err = a.service.GetComic(ctx, id)
	}

	if a.readError(w, err) {
		return
	}

	if len(comic.Metadata) != 0 {
		writeJSON(w, 200, comic.Metadata)
		return
	}
	writeJSON(w, 200, comic)
}

func (a *API) serveImage(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), a.options.StateReadTimeout)
	defer cancel()

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 1 {
		writeJSON(w, 400, map[string]string{"error": "invalid comic ID"})
		return
	}

	image, err := a.service.GetImage(ctx, id)
	if a.readError(w, err) {
		return
	}

	w.Header().Set("Content-Type", image.ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(image.Data)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(image.Data)
}

func (a *API) serveStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), a.options.StateReadTimeout)
	defer cancel()

	progress, err := a.service.Status(ctx)
	if a.readError(w, err) {
		return
	}
	writeJSON(w, 200, progress)
}

func (a *API) readError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, archive.ErrNotFound) {
		writeJSON(w, 404, map[string]string{"error": "comic or image not saved"})
		return true
	}
	a.log.Error("database read failed", "error", err)
	writeJSON(w, 503, map[string]string{"error": "database unavailable"})
	return true
}
