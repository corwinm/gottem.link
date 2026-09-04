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
