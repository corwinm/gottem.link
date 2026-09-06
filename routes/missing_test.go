package routes_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"corwinm/gottem.link/routes"
)

func TestMissingPublicSlugUsesBrandedNotFoundPage(t *testing.T) {
	database := testDatabase(t)
	router := routes.NewRouter(database, "")

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(method, "/shared-typo", nil))

			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", response.Code)
			}
			if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
				t.Errorf("Content-Type = %q", got)
			}
			if got := response.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", got)
			}
			for name, want := range map[string]string{
				"Content-Security-Policy": "default-src 'none'; style-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'",
				"X-Content-Type-Options":  "nosniff",
				"X-Frame-Options":         "DENY",
				"Referrer-Policy":         "no-referrer",
			} {
				if got := response.Header().Get(name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
			if method == http.MethodHead {
				if response.Body.Len() != 0 {
					t.Error("HEAD includes a response body")
				}
				return
			}
			markup := response.Body.String()
			for _, required := range []string{
				`<html lang="en">`, `<meta name="viewport" content="width=device-width, initial-scale=1">`,
				`<title>Link not found`, `gottem<span>.link</span>`, `That link isn't here.`,
				`Check the address you were sent`, `href="/"`, `Back to gottem.link`,
				`<link rel="stylesheet" href="/.well-known/home.css">`,
			} {
				if !strings.Contains(markup, required) {
					t.Errorf("missing-page markup missing %q", required)
				}
			}
			for _, forbidden := range []string{"shared-typo", "<script", "<form", "<input", "<style"} {
				if strings.Contains(markup, forbidden) {
					t.Errorf("missing-page markup contains %q", forbidden)
				}
			}
		})
	}
}
