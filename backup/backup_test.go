package backup_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"corwinm/gottem.link/backup"
)

func TestDecodePreservesLegacyValuesExactly(t *testing.T) {
	input := `{"version":1,"redirects":[{"slug":"Legacy_Slug","url":"/Preserve/%2F?q=A%20B","disabled":false},{"slug":"off","url":"mailto:legacy@example.com","disabled":true}]}`
	envelope, err := backup.Decode(strings.NewReader(input))
	if err != nil {
		t.Fatalf("decode export: %v", err)
	}
	if envelope.Version != 1 || len(envelope.Redirects) != 2 || envelope.Redirects[0].Slug != "Legacy_Slug" || envelope.Redirects[0].URL != "/Preserve/%2F?q=A%20B" || envelope.Redirects[1].Disabled != true {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestDecodePreservesVersionTwoLifecycleFields(t *testing.T) {
	input := `{"version":2,"redirects":[{"slug":"timed","url":"https://example.com","disabled":false,"expires_at":"2030-01-02T03:04:05Z","destination_updated_at":"2029-01-02T03:04:05Z"},{"slug":"forever","url":"https://example.org","disabled":true,"expires_at":null,"destination_updated_at":"2028-01-02T03:04:05Z"}]}`
	envelope, err := backup.Decode(strings.NewReader(input))
	if err != nil {
		t.Fatalf("decode v2 export: %v", err)
	}
	if envelope.Version != 2 || len(envelope.Redirects) != 2 || envelope.Redirects[0].ExpiresAt == nil || *envelope.Redirects[0].ExpiresAt != "2030-01-02T03:04:05Z" || envelope.Redirects[0].DestinationUpdatedAt != "2029-01-02T03:04:05Z" || envelope.Redirects[1].ExpiresAt != nil {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestDecodeRejectsNonCanonicalJSON(t *testing.T) {
	tests := map[string]string{
		"unknown envelope field": `{"version":1,"redirects":[],"extra":true}`,
		"unknown redirect field": `{"version":1,"redirects":[{"slug":"one","url":"https://example.com","disabled":false,"extra":true}]}`,
		"duplicate version":      `{"version":1,"version":1,"redirects":[]}`,
		"duplicate redirects":    `{"version":1,"redirects":[],"redirects":[]}`,
		"duplicate slug":         `{"version":1,"redirects":[{"slug":"one","slug":"two","url":"https://example.com","disabled":false}]}`,
		"duplicate URL":          `{"version":1,"redirects":[{"slug":"one","url":"https://example.com","url":"https://example.org","disabled":false}]}`,
		"duplicate disabled":     `{"version":1,"redirects":[{"slug":"one","url":"https://example.com","disabled":false,"disabled":true}]}`,
		"missing redirects":      `{"version":1}`,
		"null redirects":         `{"version":1,"redirects":null}`,
		"missing disabled":       `{"version":1,"redirects":[{"slug":"one","url":"https://example.com"}]}`,
		"trailing JSON":          `{"version":1,"redirects":[]} {}`,
		"unsupported version":    `{"version":3,"redirects":[]}`,
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
		`{"slug":"","url":"` + secretOne + `","disabled":false},` +
		`{"slug":"DUP","url":"","disabled":false},` +
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

func TestDecodeMatchesSQLiteNOCASEForDuplicateSlugs(t *testing.T) {
	input := `{"version":1,"redirects":[` +
		`{"slug":"ASCII","url":"one","disabled":false},` +
		`{"slug":"ascii","url":"two","disabled":false},` +
		`{"slug":"Ä","url":"three","disabled":false},` +
		`{"slug":"ä","url":"four","disabled":false},` +
		`{"slug":"nul\u0000x","url":"five","disabled":false},` +
		`{"slug":"NUL\u0000y","url":"six","disabled":false},` +
		`{"slug":"nul\u0000yy","url":"seven","disabled":false}]}`
	_, err := backup.Decode(strings.NewReader(input))
	var validationErr *backup.ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error = %v, want ValidationError", err)
	}
	if !reflect.DeepEqual(validationErr.Issues, []backup.Issue{
		{Index: 1, Field: "slug", Message: "duplicate slug"},
		{Index: 5, Field: "slug", Message: "duplicate slug"},
	}) {
		t.Fatalf("issues = %#v", validationErr.Issues)
	}
}
