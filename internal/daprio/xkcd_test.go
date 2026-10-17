package daprio

import (
	"net/http"
	"net/url"
	"testing"
)

func TestTimeRedirectAllowed(t *testing.T) {
	x := NewXKCD(nil, XKCDOptions{})

	target, err := url.Parse(
		"https://c.xkcd.com/redirect/comic/time",
	)
	if err != nil {
		t.Fatal(err)
	}

	original, err := http.NewRequest(
		http.MethodGet,
		"https://imgs.xkcd.com/comics/time.png",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	err = x.http.CheckRedirect(
		&http.Request{URL: target},
		[]*http.Request{original},
	)
	if err != nil {
		t.Fatalf("Time redirect was rejected: %v", err)
	}
}

func TestTimeImageRedirectUpgradesToHTTPS(t *testing.T) {
	x := NewXKCD(nil, XKCDOptions{})

	req, err := http.NewRequest(
		http.MethodGet,
		"http://imgs.xkcd.com/comics/time/example.png",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := x.http.CheckRedirect(req, nil); err != nil {
		t.Fatalf("redirect rejected: %v", err)
	}

	if req.URL.Scheme != "https" {
		t.Fatalf("scheme = %q, want https", req.URL.Scheme)
	}
}

func TestImageRedirectRejectsUntrustedHTTPHost(t *testing.T) {
	x := NewXKCD(nil, XKCDOptions{})

	req, err := http.NewRequest(
		http.MethodGet,
		"http://example.com/comic.png",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := x.http.CheckRedirect(req, nil); err == nil {
		t.Fatal("expected untrusted HTTP redirect to be rejected")
	}
}
