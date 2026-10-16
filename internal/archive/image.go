package archive

import (
	"errors"
	"fmt"
	"strings"
)

// comicImageOverrides maps comics whose advertised image URL cannot be
// downloaded directly by the archiver.
var comicImageOverrides = map[int]string{
	// 1190 "Time" uses a redirect URL rather than a normal image URL.
	1190: "https://imgs.xkcd.com/comics/time.png",
}

// ErrUnsupportedImageURL indicates an image URL that cannot be archived.
var ErrUnsupportedImageURL = errors.New("unsupported xkcd image URL")

// ResolveImageURL returns the URL to use when downloading a comic image.
func ResolveImageURL(comicID int, imageURL string) (string, error) {
	if override, ok := comicImageOverrides[comicID]; ok {
		imageURL = override
	}

	if err := validateImageURL(comicID, imageURL); err != nil {
		return "", err
	}

	return imageURL, nil
}

func validateImageURL(comicID int, imageURL string) error {
	if !strings.HasPrefix(imageURL, "https://imgs.xkcd.com/") {
		return fmt.Errorf(
			"%w: comic %d: image URL must use HTTPS on imgs.xkcd.com: %s",
			ErrUnsupportedImageURL,
			comicID,
			imageURL,
		)
	}

	return nil
}
