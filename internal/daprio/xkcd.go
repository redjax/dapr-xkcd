package daprio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	dapr "github.com/dapr/go-sdk/client"

	"github.com/redjax/dapr-xkcd/internal/archive"
)

type XKCDOptions struct {
	UserAgent         string
	RequestGap        time.Duration
	RateLimitCooldown time.Duration
	MaxImageBytes     int64
}

type XKCD struct {
	client  dapr.Client
	http    *http.Client
	options XKCDOptions

	mu          sync.Mutex
	nextRequest time.Time
}

var _ archive.ComicSource = (*XKCD)(nil)

func NewXKCD(client dapr.Client, options XKCDOptions) *XKCD {
	return &XKCD{
		client:  client,
		options: options,
		http: &http.Client{
			Timeout: 45 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many image redirects")
				}

				// XKCD sometimes returns HTTP redirect destinations.
				// Upgrade only our explicitly allowed hosts before
				// validating and sending the next request.
				if req.URL.Scheme == "http" &&
					req.URL.User == nil &&
					req.URL.Port() == "" &&
					(strings.EqualFold(req.URL.Hostname(), "imgs.xkcd.com") ||
						strings.EqualFold(req.URL.Hostname(), "c.xkcd.com")) {
					req.URL.Scheme = "https"
				}

				return validateImageURL(req.URL)
			},
		},
	}
}

func (x *XKCD) Latest(ctx context.Context) (archive.Comic, error) {
	return x.fetch(ctx, "/info.0.json")
}

func (x *XKCD) ByID(ctx context.Context, id int) (archive.Comic, error) {
	return x.fetch(ctx, fmt.Sprintf("/%d/info.0.json", id))
}

// Called with x.mu held.
func (x *XKCD) wait(ctx context.Context) error {
	if delay := time.Until(x.nextRequest); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()

		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	x.nextRequest = time.Now().Add(x.options.RequestGap)
	return nil
}

func (x *XKCD) fetch(
	ctx context.Context,
	path string,
) (archive.Comic, error) {
	var comic archive.Comic

	x.mu.Lock()
	defer x.mu.Unlock()

	if err := x.wait(ctx); err != nil {
		return comic, err
	}

	response, err := x.client.InvokeBinding(ctx, &dapr.InvokeBindingRequest{
		Name:      "xkcd",
		Operation: "get",
		Metadata: map[string]string{
			"path":       path,
			"Accept":     "application/json",
			"User-Agent": x.options.UserAgent,
		},
	})
	if err != nil {
		return comic, fmt.Errorf("invoke XKCD binding: %w", err)
	}

	status, err := strconv.Atoi(response.Metadata["statusCode"])
	if err != nil {
		return comic, fmt.Errorf("invalid upstream status metadata")
	}

	if status == http.StatusTooManyRequests {
		var retryAfter string
		for key, value := range response.Metadata {
			if strings.EqualFold(key, "Retry-After") {
				retryAfter = value
				break
			}
		}
		x.rateLimit(retryAfter)
	}

	if status != http.StatusOK {
		return comic, fmt.Errorf("XKCD %s returned HTTP %d", path, status)
	}

	if err := json.Unmarshal(response.Data, &comic); err != nil {
		return comic, fmt.Errorf("decode XKCD response: %w", err)
	}
	if comic.Num < 1 {
		return comic, fmt.Errorf("response has no valid comic number")
	}

	comic.Metadata = append(json.RawMessage(nil), response.Data...)
	return comic, nil
}

func (x *XKCD) DownloadImage(
	ctx context.Context,
	imageURL string,
) (archive.Image, error) {
	var image archive.Image

	parsed, err := url.Parse(imageURL)
	if err != nil {
		return image, err
	}
	if err := validateImageURL(parsed); err != nil {
		return image, err
	}

	x.mu.Lock()
	defer x.mu.Unlock()

	if err := x.wait(ctx); err != nil {
		return image, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return image, err
	}
	req.Header.Set("User-Agent", x.options.UserAgent)
	req.Header.Set("Accept", "image/*")

	response, err := x.http.Do(req)
	if err != nil {
		return image, fmt.Errorf("download image: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusTooManyRequests {
		x.rateLimit(response.Header.Get("Retry-After"))
	}
	if response.StatusCode != http.StatusOK {
		return image, fmt.Errorf("image returned HTTP %d", response.StatusCode)
	}

	if response.ContentLength > x.options.MaxImageBytes {
		return image, fmt.Errorf("image exceeds configured size limit")
	}

	data, err := io.ReadAll(io.LimitReader(
		response.Body,
		x.options.MaxImageBytes+1,
	))
	if err != nil {
		return image, err
	}
	if len(data) == 0 || int64(len(data)) > x.options.MaxImageBytes {
		return image, fmt.Errorf("image is empty or exceeds configured size limit")
	}

	contentType, _, err := mime.ParseMediaType(
		response.Header.Get("Content-Type"),
	)
	if err != nil || contentType == "application/octet-stream" {
		contentType = http.DetectContentType(data)
	}
	if !strings.HasPrefix(contentType, "image/") {
		return image, fmt.Errorf("unexpected image content type %q", contentType)
	}

	sum := sha256.Sum256(data)

	return archive.Image{
		Data:        data,
		ContentType: contentType,
		SHA256:      hex.EncodeToString(sum[:]),
	}, nil
}

func validateImageURL(u *url.URL) error {
	// Require encrypted requests without embedded credentials
	// or nonstandard ports.
	if u.Scheme != "https" ||
		u.User != nil ||
		(u.Port() != "" && u.Port() != "443") {
		return fmt.Errorf(
			"image URL must use HTTPS without credentials on an allowed XKCD host: %s",
			u.Redacted(),
		)
	}

	// Normal image files.
	if strings.EqualFold(u.Hostname(), "imgs.xkcd.com") {
		return nil
	}

	// XKCD's comic-image redirect service, including Time and Now.
	if strings.EqualFold(u.Hostname(), "c.xkcd.com") &&
		strings.HasPrefix(u.EscapedPath(), "/redirect/comic/") &&
		len(u.EscapedPath()) > len("/redirect/comic/") {
		return nil
	}

	return fmt.Errorf(
		"image URL must use imgs.xkcd.com or c.xkcd.com/redirect/comic/: %s",
		u.Redacted(),
	)
}

// Called with x.mu held.
func (x *XKCD) rateLimit(value string) {
	now := time.Now()
	cooldown := now.Add(x.options.RateLimitCooldown)

	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil &&
		seconds >= 0 &&
		seconds <= int64((1<<63-1)/int64(time.Second)) {
		cooldown = now.Add(time.Duration(seconds) * time.Second)
	} else if date, err := http.ParseTime(value); err == nil {
		cooldown = date
	}

	if cooldown.After(x.nextRequest) {
		x.nextRequest = cooldown
	}
}
