package validation

import (
	"errors"
	"net/url"
	"strings"
	"unicode"
)

const (
	maxDestinationBytes = 2048
	maxSlugLength       = 64
)

var (
	ErrInvalidDestination = errors.New("invalid destination")
	ErrInvalidSlug        = errors.New("invalid slug")
	ErrReservedSlug       = errors.New("reserved slug")
)

func ValidateDestination(destination string) (string, error) {
	if destination == "" || len(destination) > maxDestinationBytes || containsControlCharacter(destination) {
		return "", ErrInvalidDestination
	}

	parsed, err := url.Parse(destination)
	if err != nil || (!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) || parsed.Hostname() == "" || parsed.User != nil {
		return "", ErrInvalidDestination
	}

	return destination, nil
}

func ValidateSlug(slug string) (string, error) {
	canonical := strings.ToLower(slug)
	if canonical == "api" || strings.HasPrefix(canonical, ".well-known") {
		return "", ErrReservedSlug
	}
	if len(slug) == 0 || len(slug) > maxSlugLength {
		return "", ErrInvalidSlug
	}

	result := make([]byte, len(slug))
	for i := range len(slug) {
		character := slug[i]
		switch {
		case character >= 'a' && character <= 'z', character >= '0' && character <= '9':
			result[i] = character
		case character >= 'A' && character <= 'Z':
			result[i] = character + ('a' - 'A')
		case character == '-' && i > 0 && i < len(slug)-1 && slug[i-1] != '-':
			result[i] = character
		default:
			return "", ErrInvalidSlug
		}
	}

	return string(result), nil
}

func containsControlCharacter(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}
