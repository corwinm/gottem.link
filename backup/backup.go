package backup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	Version  = 1
	MaxBytes = 1 << 20
)

var ErrTooLarge = errors.New("import exceeds 1 MiB")

type Envelope struct {
	Version   int        `json:"version"`
	Redirects []Redirect `json:"redirects"`
}

type Redirect struct {
	Slug     string `json:"slug"`
	URL      string `json:"url"`
	Disabled bool   `json:"disabled"`
}

type Issue struct {
	Index   int    `json:"index"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ValidationError struct {
	Issues []Issue
}

func (err *ValidationError) Error() string {
	parts := make([]string, 0, len(err.Issues))
	for _, issue := range err.Issues {
		parts = append(parts, fmt.Sprintf("redirect %d %s: %s", issue.Index, issue.Field, issue.Message))
	}
	return "invalid redirects: " + strings.Join(parts, "; ")
}

func Decode(reader io.Reader) (Envelope, error) {
	data, err := io.ReadAll(io.LimitReader(reader, MaxBytes+1))
	if err != nil {
		return Envelope{}, errors.New("read import")
	}
	if len(data) > MaxBytes {
		return Envelope{}, ErrTooLarge
	}

	var envelope Envelope
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return Envelope{}, errors.New("invalid import JSON")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Envelope{}, errors.New("import must contain exactly one JSON value")
	}
	if envelope.Version != Version {
		return Envelope{}, fmt.Errorf("unsupported import version %d", envelope.Version)
	}
	if err := validate(&envelope); err != nil {
		return Envelope{}, err
	}
	return envelope, nil
}

func validate(envelope *Envelope) error {
	issues := make([]Issue, 0)
	seen := make(map[sqliteNOCASEKey]int)
	for index := range envelope.Redirects {
		redirect := &envelope.Redirects[index]
		if redirect.Slug == "" {
			issues = append(issues, Issue{Index: index, Field: "slug", Message: "empty slug"})
		} else {
			key := sqliteNOCASE(redirect.Slug)
			if _, exists := seen[key]; exists {
				issues = append(issues, Issue{Index: index, Field: "slug", Message: "duplicate slug"})
			} else {
				seen[key] = index
			}
		}
		if redirect.URL == "" {
			issues = append(issues, Issue{Index: index, Field: "url", Message: "empty URL"})
		}
	}
	if len(issues) > 0 {
		return &ValidationError{Issues: issues}
	}
	return nil
}

type sqliteNOCASEKey struct {
	prefix string
	length int
}

func sqliteNOCASE(value string) sqliteNOCASEKey {
	prefix := value
	if nul := strings.IndexByte(prefix, 0); nul >= 0 {
		prefix = prefix[:nul]
	}
	folded := []byte(prefix)
	for index, character := range folded {
		if character >= 'A' && character <= 'Z' {
			folded[index] = character + ('a' - 'A')
		}
	}
	return sqliteNOCASEKey{prefix: string(folded), length: len(value)}
}
