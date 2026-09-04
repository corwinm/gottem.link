package handlers

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestGenerateSlugFromZeroReader(t *testing.T) {
	got, err := generateSlug(bytes.NewReader(make([]byte, 5)))
	if err != nil {
		t.Fatalf("generate slug: %v", err)
	}
	if got != "aaaaaaa" {
		t.Fatalf("slug = %q, want aaaaaaa", got)
	}
}

func TestGenerateSlugShape(t *testing.T) {
	got, err := GenerateSlug()
	if err != nil {
		t.Fatalf("generate slug: %v", err)
	}
	if len(got) != 7 {
		t.Fatalf("slug length = %d, want 7", len(got))
	}
	for _, character := range got {
		if (character < 'a' || character > 'z') && (character < '2' || character > '7') {
			t.Fatalf("slug %q contains invalid character %q", got, character)
		}
	}
}

func TestGenerateSlugReaderFailures(t *testing.T) {
	tests := []struct {
		name   string
		reader io.Reader
	}{
		{name: "short reader", reader: bytes.NewReader(make([]byte, 4))},
		{name: "error reader", reader: errorReader{err: errors.New("random source failed")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := generateSlug(test.reader); err == nil {
				t.Fatal("generateSlug error = nil, want error")
			}
		})
	}
}

type errorReader struct {
	err error
}

func (reader errorReader) Read([]byte) (int, error) {
	return 0, reader.err
}
