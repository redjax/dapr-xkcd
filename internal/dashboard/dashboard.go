package dashboard

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"time"
)

//go:embed templates/dashboard.html
var templates embed.FS

type RecentComic struct {
	ID           int
	Title        string
	DownloadedAt time.Time
}

type Failure struct {
	ComicID       int
	Attempts      int
	LastAttemptAt time.Time
	Message       string
}

type Stats struct {
	CompleteCount    int64
	PendingCount     int64
	RetryCount       int64
	UnavailableCount int64
	ImageBytes       int64

	LatestSeenID  int
	LatestSavedID int
	NextID        int
	ProgressAt    time.Time

	RecentComics []RecentComic
	Failures     []Failure
}

type Source interface {
	DashboardStats(ctx context.Context) (Stats, error)
}

type Handler struct {
	source  Source
	log     *slog.Logger
	timeout time.Duration
	started time.Time
	tmpl    *template.Template
}

type view struct {
	Stats
	Uptime      string
	RefreshedAt time.Time
	Error       string
}

func New(
	source Source,
	log *slog.Logger,
	timeout time.Duration,
) (*Handler, error) {
	tmpl, err := template.New("dashboard.html").
		Funcs(template.FuncMap{
			"bytes": formatBytes,
			"time":  formatTime,
		}).
		ParseFS(templates, "templates/dashboard.html")
	if err != nil {
		return nil, fmt.Errorf("parse dashboard template: %w", err)
	}

	return &Handler{
		source:  source,
		log:     log,
		timeout: timeout,
		started: time.Now(),
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

	data := view{
		Uptime:      time.Since(h.started).Truncate(time.Second).String(),
		RefreshedAt: time.Now().UTC(),
	}

	status := http.StatusOK
	stats, err := h.source.DashboardStats(ctx)
	if err != nil {
		h.log.Error("load dashboard statistics", "error", err)
		data.Error = "Unable to read application statistics from PostgreSQL."
		status = http.StatusServiceUnavailable
	} else {
		data.Stats = stats
	}

	var output bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&output, "dashboard.html", data); err != nil {
		h.log.Error("render dashboard", "error", err)
		http.Error(w, "Unable to render dashboard", http.StatusInternalServerError)
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

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "Never"
	}
	return value.UTC().Format("2006-01-02 15:04:05 UTC")
}

func formatBytes(value int64) string {
	if value < 1024 {
		return fmt.Sprintf("%d B", value)
	}

	size := float64(value)
	units := []string{"KiB", "MiB", "GiB", "TiB"}

	for _, unit := range units {
		size /= 1024
		if size < 1024 || unit == "TiB" {
			return fmt.Sprintf("%.1f %s", size, unit)
		}
	}

	return fmt.Sprintf("%d B", value)
}
