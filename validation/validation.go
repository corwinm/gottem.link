package validation

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
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
	if destination == "" || len(destination) > maxDestinationBytes || !utf8.ValidString(destination) || containsUnsafeCharacter(destination) {
		return "", ErrInvalidDestination
	}

	parsed, err := url.Parse(destination)
	if err != nil || (!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) || parsed.User != nil || !validAuthority(parsed) {
		return "", ErrInvalidDestination
	}

	return destination, nil
}

func ValidateSlug(slug string) (string, error) {
	if len(slug) == 0 || len(slug) > maxSlugLength {
		return "", ErrInvalidSlug
	}
	canonical := strings.ToLower(slug)
	if canonical == "api" || strings.HasPrefix(canonical, ".well-known") {
		return "", ErrReservedSlug
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

func validAuthority(parsed *url.URL) bool {
	hostname := parsed.Hostname()
	if hostname == "" || !validHostname(hostname) {
		return false
	}

	port, explicit := explicitPort(parsed.Host)
	if !explicit {
		return true
	}
	portNumber, err := strconv.Atoi(port)
	return err == nil && portNumber >= 1 && portNumber <= 65535
}

func explicitPort(host string) (string, bool) {
	if strings.HasPrefix(host, "[") {
		closingBracket := strings.LastIndexByte(host, ']')
		if closingBracket < 0 || closingBracket == len(host)-1 {
			return "", false
		}
		return host[closingBracket+2:], true
	}

	colon := strings.LastIndexByte(host, ':')
	if colon < 0 {
		return "", false
	}
	return host[colon+1:], true
}

func validHostname(hostname string) bool {
	if net.ParseIP(hostname) != nil {
		return true
	}
	if len(hostname) > 253 || isDottedNumeric(hostname) {
		return false
	}

	for _, label := range strings.Split(hostname, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := range len(label) {
			character := label[i]
			if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}

func isDottedNumeric(hostname string) bool {
	if !strings.Contains(hostname, ".") {
		return false
	}
	for i := range len(hostname) {
		if hostname[i] != '.' && (hostname[i] < '0' || hostname[i] > '9') {
			return false
		}
	}
	return true
}

func containsUnsafeCharacter(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) || unicode.IsSpace(character) || unicode.In(character, unicode.Cf) {
			return true
		}
	}
	return false
}
