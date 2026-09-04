package validation_test

import (
	"errors"
	"strings"
	"testing"

	"corwinm/gottem.link/validation"
)

func TestValidateDestinationAcceptsAbsoluteHTTPURLsWithoutChangingThem(t *testing.T) {
	maxLengthURL := "https://example.com/" + strings.Repeat("a", 2048-len("https://example.com/"))
	tests := []string{
		"http://example.com",
		"https://example.com/path?query=value#fragment",
		"https://Example.COM:8443/a%2Fb?x=One#Top",
		"http://127.0.0.1:1",
		"https://127.0.0.1:65535/resource",
		"https://[2001:db8::1]/resource",
		"https://example.com/%E2%80%8B",
		maxLengthURL,
	}

	for _, input := range tests {
		t.Run(input[:min(len(input), 80)], func(t *testing.T) {
			got, err := validation.ValidateDestination(input)
			if err != nil {
				t.Fatalf("ValidateDestination(%q) error = %v", input, err)
			}
			if got != input {
				t.Fatalf("ValidateDestination(%q) = %q, want original input", input, got)
			}
		})
	}
}

func TestValidateDestinationRejectsInvalidURLs(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "empty", input: ""},
		{name: "relative", input: "example.com/path"},
		{name: "scheme relative", input: "//example.com/path"},
		{name: "unsupported ftp scheme", input: "ftp://example.com/file"},
		{name: "unsupported javascript scheme", input: "javascript:alert(1)"},
		{name: "missing host", input: "https:///path"},
		{name: "userinfo", input: "https://user@example.com/path"},
		{name: "credentials", input: "https://user:secret@example.com/path"},
		{name: "empty userinfo", input: "https://@example.com/path"},
		{name: "newline control", input: "https://example.com/a\nb"},
		{name: "nul control", input: "https://example.com/a\x00b"},
		{name: "delete control", input: "https://example.com/a\x7fb"},
		{name: "unicode control", input: "https://example.com/a\u0085b"},
		{name: "malformed UTF-8", input: "https://example.com/a\xffb"},
		{name: "unicode format character", input: "https://example.com/a\u200bb"},
		{name: "unicode line separator", input: "https://example.com/a\u2028b"},
		{name: "unicode paragraph separator", input: "https://example.com/a\u2029b"},
		{name: "empty port", input: "https://example.com:/path"},
		{name: "zero port", input: "https://example.com:0/path"},
		{name: "port above range", input: "https://example.com:65536/path"},
		{name: "large port above range", input: "https://example.com:99999/path"},
		{name: "hyphen host", input: "https://-/path"},
		{name: "leading host label hyphen", input: "https://-example.com/path"},
		{name: "trailing host label hyphen", input: "https://example-.com/path"},
		{name: "empty host label", input: "https://example..com/path"},
		{name: "trailing empty host label", input: "https://example.com./path"},
		{name: "oversized host label", input: "https://" + strings.Repeat("a", 64) + ".com/path"},
		{name: "invalid dotted numeric address", input: "https://127.0.0.999/path"},
		{name: "non-ASCII hostname", input: "https://café.example/path"},
		{name: "invisible hostname character", input: "https://exam\u200bple.com/path"},
		{name: "oversized", input: "https://example.com/" + strings.Repeat("a", 2048)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := validation.ValidateDestination(test.input)
			if !errors.Is(err, validation.ErrInvalidDestination) {
				t.Fatalf("ValidateDestination(%q) error = %v, want ErrInvalidDestination", test.input, err)
			}
			if got != "" {
				t.Fatalf("ValidateDestination(%q) = %q, want empty result", test.input, got)
			}
		})
	}
}

func TestValidateSlugAcceptsAndCanonicalizesASCII(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "a", want: "a"},
		{input: "short-link", want: "short-link"},
		{input: "Release-2026", want: "release-2026"},
		{input: "ABC123", want: "abc123"},
		{input: strings.Repeat("a", 64), want: strings.Repeat("a", 64)},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			got, err := validation.ValidateSlug(test.input)
			if err != nil {
				t.Fatalf("ValidateSlug(%q) error = %v", test.input, err)
			}
			if got != test.want {
				t.Fatalf("ValidateSlug(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestValidateSlugRejectsInvalidSyntax(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "empty", input: ""},
		{name: "too long", input: strings.Repeat("a", 65)},
		{name: "leading hyphen", input: "-slug"},
		{name: "trailing hyphen", input: "slug-"},
		{name: "repeated hyphen", input: "two--words"},
		{name: "space", input: "two words"},
		{name: "leading whitespace is not trimmed", input: " slug"},
		{name: "trailing whitespace is not trimmed", input: "slug "},
		{name: "tab", input: "two\twords"},
		{name: "newline", input: "two\nwords"},
		{name: "punctuation", input: "slug_name"},
		{name: "dot", input: "slug.name"},
		{name: "non ASCII letter", input: "café"},
		{name: "emoji", input: "slug😀"},
		{name: "control character", input: "slug\x00name"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := validation.ValidateSlug(test.input)
			if !errors.Is(err, validation.ErrInvalidSlug) {
				t.Fatalf("ValidateSlug(%q) error = %v, want ErrInvalidSlug", test.input, err)
			}
			if got != "" {
				t.Fatalf("ValidateSlug(%q) = %q, want empty result", test.input, got)
			}
		})
	}
}

func TestValidateSlugChecksLengthBeforeReservedNamespace(t *testing.T) {
	input := ".well-known" + strings.Repeat("a", 64)

	got, err := validation.ValidateSlug(input)
	if !errors.Is(err, validation.ErrInvalidSlug) {
		t.Fatalf("ValidateSlug(%q) error = %v, want ErrInvalidSlug", input, err)
	}
	if got != "" {
		t.Fatalf("ValidateSlug(%q) = %q, want empty result", input, got)
	}
}

func TestValidateSlugRejectsReservedSlugs(t *testing.T) {
	tests := []string{
		"api",
		"API",
		".well-known",
		".well-known/healthz",
		".WELL-KNOWN-anything",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			got, err := validation.ValidateSlug(input)
			if !errors.Is(err, validation.ErrReservedSlug) {
				t.Fatalf("ValidateSlug(%q) error = %v, want ErrReservedSlug", input, err)
			}
			if got != "" {
				t.Fatalf("ValidateSlug(%q) = %q, want empty result", input, got)
			}
		})
	}
}
