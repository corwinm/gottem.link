package backup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	LegacyVersion    = 1
	LifecycleVersion = 2
	Version          = 3
	MaxBytes         = 1 << 20
)

var ErrTooLarge = errors.New("import exceeds 1 MiB")

type Envelope struct {
	Version   int        `json:"version"`
	Redirects []Redirect `json:"redirects"`
}

type Redirect struct {
	Slug                 string  `json:"slug"`
	URL                  string  `json:"url"`
	Disabled             bool    `json:"disabled"`
	ExpiresAt            *string `json:"expires_at"`
	DestinationUpdatedAt string  `json:"destination_updated_at"`
	ClickCount           int64   `json:"click_count"`
	LastAccessedAt       *string `json:"last_accessed_at"`
}

func (envelope Envelope) MarshalJSON() ([]byte, error) {
	if envelope.Version == LegacyVersion {
		type legacyRedirect struct {
			Slug     string `json:"slug"`
			URL      string `json:"url"`
			Disabled bool   `json:"disabled"`
		}
		redirects := make([]legacyRedirect, len(envelope.Redirects))
		for index, redirect := range envelope.Redirects {
			redirects[index] = legacyRedirect{Slug: redirect.Slug, URL: redirect.URL, Disabled: redirect.Disabled}
		}
		return json.Marshal(struct {
			Version   int              `json:"version"`
			Redirects []legacyRedirect `json:"redirects"`
		}{Version: envelope.Version, Redirects: redirects})
	}
	if envelope.Version == LifecycleVersion {
		type lifecycleRedirect struct {
			Slug                 string  `json:"slug"`
			URL                  string  `json:"url"`
			Disabled             bool    `json:"disabled"`
			ExpiresAt            *string `json:"expires_at"`
			DestinationUpdatedAt string  `json:"destination_updated_at"`
		}
		redirects := make([]lifecycleRedirect, len(envelope.Redirects))
		for index, redirect := range envelope.Redirects {
			redirects[index] = lifecycleRedirect{Slug: redirect.Slug, URL: redirect.URL, Disabled: redirect.Disabled, ExpiresAt: redirect.ExpiresAt, DestinationUpdatedAt: redirect.DestinationUpdatedAt}
		}
		return json.Marshal(struct {
			Version   int                 `json:"version"`
			Redirects []lifecycleRedirect `json:"redirects"`
		}{Version: envelope.Version, Redirects: redirects})
	}
	type currentEnvelope Envelope
	return json.Marshal(currentEnvelope(envelope))
}

type versionEnvelope struct {
	Version int `json:"version"`
}

type wireEnvelopeV1 struct {
	Version   int               `json:"version"`
	Redirects *[]wireRedirectV1 `json:"redirects"`
}

type wireRedirectV1 struct {
	Slug     string `json:"slug"`
	URL      string `json:"url"`
	Disabled *bool  `json:"disabled"`
}

type wireEnvelopeV2 struct {
	Version   int               `json:"version"`
	Redirects *[]wireRedirectV2 `json:"redirects"`
}

type wireRedirectV2 struct {
	Slug                 string          `json:"slug"`
	URL                  string          `json:"url"`
	Disabled             *bool           `json:"disabled"`
	ExpiresAt            json.RawMessage `json:"expires_at"`
	DestinationUpdatedAt *string         `json:"destination_updated_at"`
}

type wireEnvelopeV3 struct {
	Version   int               `json:"version"`
	Redirects *[]wireRedirectV3 `json:"redirects"`
}

type wireRedirectV3 struct {
	Slug                 string          `json:"slug"`
	URL                  string          `json:"url"`
	Disabled             *bool           `json:"disabled"`
	ExpiresAt            json.RawMessage `json:"expires_at"`
	DestinationUpdatedAt *string         `json:"destination_updated_at"`
	ClickCount           *int64          `json:"click_count"`
	LastAccessedAt       json.RawMessage `json:"last_accessed_at"`
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
	if err := rejectDuplicateObjectFields(data); err != nil {
		return Envelope{}, errors.New("invalid import JSON")
	}

	var version versionEnvelope
	if err := json.Unmarshal(data, &version); err != nil {
		return Envelope{}, errors.New("invalid import JSON")
	}
	switch version.Version {
	case LegacyVersion:
		var wire wireEnvelopeV1
		if err := decodeStrict(data, &wire); err != nil || wire.Redirects == nil {
			return Envelope{}, errors.New("invalid import JSON")
		}
		envelope := Envelope{Version: wire.Version, Redirects: make([]Redirect, len(*wire.Redirects))}
		for index, redirect := range *wire.Redirects {
			if redirect.Disabled == nil {
				return Envelope{}, errors.New("invalid import JSON")
			}
			envelope.Redirects[index] = Redirect{Slug: redirect.Slug, URL: redirect.URL, Disabled: *redirect.Disabled}
		}
		if err := validate(&envelope); err != nil {
			return Envelope{}, err
		}
		return envelope, nil
	case LifecycleVersion:
		var wire wireEnvelopeV2
		if err := decodeStrict(data, &wire); err != nil || wire.Redirects == nil {
			return Envelope{}, errors.New("invalid import JSON")
		}
		envelope := Envelope{Version: wire.Version, Redirects: make([]Redirect, len(*wire.Redirects))}
		for index, redirect := range *wire.Redirects {
			if redirect.Disabled == nil || redirect.ExpiresAt == nil || redirect.DestinationUpdatedAt == nil {
				return Envelope{}, errors.New("invalid import JSON")
			}
			var expiresAt *string
			if string(redirect.ExpiresAt) != "null" {
				var raw string
				if err := json.Unmarshal(redirect.ExpiresAt, &raw); err != nil {
					return Envelope{}, errors.New("invalid import JSON")
				}
				expiresAt = &raw
			}
			envelope.Redirects[index] = Redirect{Slug: redirect.Slug, URL: redirect.URL, Disabled: *redirect.Disabled, ExpiresAt: expiresAt, DestinationUpdatedAt: *redirect.DestinationUpdatedAt}
		}
		if err := validate(&envelope); err != nil {
			return Envelope{}, err
		}
		return envelope, nil
	case Version:
		var wire wireEnvelopeV3
		if err := decodeStrict(data, &wire); err != nil || wire.Redirects == nil {
			return Envelope{}, errors.New("invalid import JSON")
		}
		envelope := Envelope{Version: wire.Version, Redirects: make([]Redirect, len(*wire.Redirects))}
		for index, redirect := range *wire.Redirects {
			if redirect.Disabled == nil || redirect.ExpiresAt == nil || redirect.DestinationUpdatedAt == nil || redirect.ClickCount == nil || redirect.LastAccessedAt == nil {
				return Envelope{}, errors.New("invalid import JSON")
			}
			var expiresAt, lastAccessedAt *string
			if string(redirect.ExpiresAt) != "null" {
				var raw string
				if err := json.Unmarshal(redirect.ExpiresAt, &raw); err != nil {
					return Envelope{}, errors.New("invalid import JSON")
				}
				expiresAt = &raw
			}
			if string(redirect.LastAccessedAt) != "null" {
				var raw string
				if err := json.Unmarshal(redirect.LastAccessedAt, &raw); err != nil {
					return Envelope{}, errors.New("invalid import JSON")
				}
				lastAccessedAt = &raw
			}
			envelope.Redirects[index] = Redirect{Slug: redirect.Slug, URL: redirect.URL, Disabled: *redirect.Disabled, ExpiresAt: expiresAt, DestinationUpdatedAt: *redirect.DestinationUpdatedAt, ClickCount: *redirect.ClickCount, LastAccessedAt: lastAccessedAt}
		}
		if err := validate(&envelope); err != nil {
			return Envelope{}, err
		}
		return envelope, nil
	default:
		return Envelope{}, fmt.Errorf("unsupported import version %d", version.Version)
	}
}

func decodeStrict(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("import must contain exactly one JSON value")
	}
	return nil
}

func rejectDuplicateObjectFields(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("object key is not a string")
				}
				if _, exists := seen[key]; exists {
					return errors.New("duplicate object field")
				}
				seen[key] = struct{}{}
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		default:
			return errors.New("unexpected JSON delimiter")
		}
	}
	return walk()
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
		if envelope.Version >= LifecycleVersion {
			if redirect.ExpiresAt != nil {
				if _, err := time.Parse(time.RFC3339, *redirect.ExpiresAt); err != nil {
					issues = append(issues, Issue{Index: index, Field: "expires_at", Message: "invalid timestamp"})
				}
			}
			if _, err := time.Parse(time.RFC3339, redirect.DestinationUpdatedAt); err != nil {
				issues = append(issues, Issue{Index: index, Field: "destination_updated_at", Message: "invalid timestamp"})
			}
		}
		if envelope.Version == Version {
			if redirect.ClickCount < 0 {
				issues = append(issues, Issue{Index: index, Field: "click_count", Message: "must not be negative"})
			}
			if (redirect.ClickCount == 0) != (redirect.LastAccessedAt == nil) {
				issues = append(issues, Issue{Index: index, Field: "last_accessed_at", Message: "must be present exactly when click_count is nonzero"})
			}
			if redirect.LastAccessedAt != nil {
				if _, err := time.Parse(time.RFC3339, *redirect.LastAccessedAt); err != nil {
					issues = append(issues, Issue{Index: index, Field: "last_accessed_at", Message: "invalid timestamp"})
				}
			}
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
