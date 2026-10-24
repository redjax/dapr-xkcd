package daprio

import (
	"net/http"
	"testing"
)

func TestComicRedirectPolicy(t *testing.T) {
	x := NewXKCD(nil, XKCDOptions{})

	tests := []struct {
		name    string
		rawURL  string
		allowed bool
	}{
		{
			name:    "Time redirect",
			rawURL:  "https://c.xkcd.com/redirect/comic/time",
			allowed: true,
		},
		{
			name:    "Now redirect",
			rawURL:  "https://c.xkcd.com/redirect/comic/now",
			allowed: true,
		},
		{
			name:    "HTTP image upgraded",
			rawURL:  "http://imgs.xkcd.com/comics/time/example.png",
			allowed: true,
		},
		{
			name:    "HTTP comic redirect upgraded",
			rawURL:  "http://c.xkcd.com/redirect/comic/now",
			allowed: true,
		},
		{
			name:    "unrelated host rejected",
			rawURL:  "https://example.com/image.png",
			allowed: false,
		},
		{
			name:    "unrelated redirect-service path rejected",
			rawURL:  "https://c.xkcd.com/other",
			allowed: false,
		},
		{
			name:    "nonstandard port rejected",
			rawURL:  "https://imgs.xkcd.com:8443/image.png",
			allowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, tt.rawURL, nil)
			if err != nil {
				t.Fatal(err)
			}

			err = x.http.CheckRedirect(req, nil)
			if (err == nil) != tt.allowed {
				t.Fatalf(
					"redirect error = %v; allowed = %v",
					err,
					tt.allowed,
				)
			}

			if tt.allowed && req.URL.Scheme != "https" {
				t.Fatalf(
					"scheme = %q; want https",
					req.URL.Scheme,
				)
			}
		})
	}
}
