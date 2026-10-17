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
