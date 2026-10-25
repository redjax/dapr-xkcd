package archive

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrBusy     = errors.New("ingestion is already running")
	ErrNotFound = errors.New("comic or image not saved")
)

type Comic struct {
	Num        int    `json:"num"`
	Title      string `json:"title"`
	SafeTitle  string `json:"safe_title"`
	Alt        string `json:"alt"`
	Img        string `json:"img"`
	Day        string `json:"day"`
	Month      string `json:"month"`
	Year       string `json:"year"`
	Link       string `json:"link"`
	News       string `json:"news"`
	Transcript string `json:"transcript"`

	Metadata json.RawMessage `json:"-"`
}

type Image struct {
	Data        []byte
	ContentType string
	SHA256      string
}

type Progress struct {
	LatestSeenID  int       `json:"latest_seen_id"`
	LatestSavedID int       `json:"latest_saved_id"`
	NextID        int       `json:"next_id"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ComicSource interface {
	Latest(ctx context.Context) (Comic, error)
	ByID(ctx context.Context, id int) (Comic, error)
	DownloadImage(ctx context.Context, imageURL string) (Image, error)
}

type Store interface {
	GetComic(ctx context.Context, id int) (Comic, error)
	GetImage(ctx context.Context, id int) (Image, error)
	LoadProgress(ctx context.Context) (Progress, error)
	IsComplete(ctx context.Context, id int) (bool, error)

	RetryManaged(ctx context.Context, id int) (bool, error)

	ObserveLatest(ctx context.Context, id int) error
	MarkAttempt(ctx context.Context, id int) error
	MarkFailed(ctx context.Context, id int, message string) error

	SaveComplete(
		ctx context.Context,
		comic Comic,
		image Image,
		progress Progress,
	) error

	SaveRetryComplete(
		ctx context.Context,
		comic Comic,
		image Image,
	) error

	SaveSkipped(
		ctx context.Context,
		id int,
		reason string,
		progress Progress,
	) error

	SaveProgress(ctx context.Context, progress Progress) error

	QueueBackfillFailure(
		ctx context.Context,
		id int,
		message string,
		progress Progress,
	) error

	ClaimRetry(ctx context.Context) (int, error)

	FinishRetryFailure(
		ctx context.Context,
		id int,
		message string,
	) error
}
