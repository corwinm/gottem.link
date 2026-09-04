package backup_test

import (
	"errors"
	"strings"
	"testing"

	"corwinm/gottem.link/backup"
)

func TestDecodeAcceptsAndCanonicalizesVersionOneEnvelope(t *testing.T) {
	input := `{"version":1,"redirects":[{"slug":"Active","url":"https://example.com/a","disabled":false},{"slug":"off","url":"https://example.com/o","disabled":true}]}`
	envelope, err := backup.Decode(strings.NewReader(input))
	if err != nil {
		t.Fatalf("decode export: %v", err)
	}
	if envelope.Version != 1 || len(envelope.Redirects) != 2 || envelope.Redirects[0].Slug != "active" || envelope.Redirects[1].Disabled != true {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestDecodeRejectsNonCanonicalJSON(t *testing.T) {
	tests := map[string]string{
		"unknown envelope field": `{"version":1,"redirects":[],"extra":true}`,
		"unknown redirect field": `{"version":1,"redirects":[{"slug":"one","url":"https://example.com","disabled":false,"extra":true}]}`,
		"trailing JSON":          `{"version":1,"redirects":[]} {}`,
		"unsupported version":    `{"version":2,"redirects":[]}`,
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := backup.Decode(strings.NewReader(input)); err == nil {
				t.Fatal("decode succeeded")
			}
		})
	}
}

func TestDecodeCapsInputAtOneMiB(t *testing.T) {
	input := `{"version":1,"redirects":[],"padding":"` + strings.Repeat("x", backup.MaxBytes) + `"}`
	if _, err := backup.Decode(strings.NewReader(input)); !errors.Is(err, backup.ErrTooLarge) {
		t.Fatalf("error = %v, want ErrTooLarge", err)
	}
}

func TestDecodeReportsAllSemanticIssuesWithoutDestinations(t *testing.T) {
	secretOne := "https://secret.example/one"
	secretTwo := "https://secret.example/two"
	input := `{"version":1,"redirects":[` +
		`{"slug":"bad slug","url":"` + secretOne + `","disabled":false},` +
		`{"slug":"DUP","url":"not-a-url","disabled":false},` +
		`{"slug":"dup","url":"` + secretTwo + `","disabled":true}]}`
	_, err := backup.Decode(strings.NewReader(input))
	var validationErr *backup.ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error = %v, want ValidationError", err)
	}
	if len(validationErr.Issues) != 3 {
		t.Fatalf("issues = %#v, want 3", validationErr.Issues)
	}
	if strings.Contains(err.Error(), secretOne) || strings.Contains(err.Error(), secretTwo) {
		t.Fatalf("validation error leaked destination: %v", err)
	}
}
