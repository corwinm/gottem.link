package routes_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"corwinm/gottem.link/routes"
)

func TestPublicHomepage(t *testing.T) {
	// The public page must work without a database or admin configuration.
	router := routes.NewRouter(nil, "")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/?from=shared", nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("homepage status/content-type = %d/%q, want 200/text/html; charset=utf-8", response.Code, response.Header().Get("Content-Type"))
	}
	markup := response.Body.String()
	for _, required := range []string{`<html lang="en">`, `<meta name="viewport" content="width=device-width, initial-scale=1">`, `<title>gottem.link`, `<main`, `<h1>gottem<span>.link</span></h1>`, `A personal`, `URL shortener.`, `Open the complete address you were sent`, `Short link you received`, `gottem<span>.link</span>/example`, `opens`, `Original website`, `example.com/article`, `Links are not listed publicly.`, `href="/admin/"`, `>Admin</a>`, `<link rel="stylesheet" href="/.well-known/home.css">`} {
		if !strings.Contains(markup, required) {
			t.Errorf("homepage missing %q", required)
		}
	}
	for _, forbidden := range []string{"Hello, World!", "Short links. Straight there.", "Admin sign in", "<script", "<form", "<input", "<style", "https://", "http://", "/api/"} {
		if strings.Contains(markup, forbidden) {
			t.Errorf("homepage contains %q", forbidden)
		}
	}
	if strings.Count(markup, "<a ") != 1 {
		t.Error("homepage should expose only the admin sign-in link")
	}
}

func TestPublicHomepageResources(t *testing.T) {
	router := routes.NewRouter(nil, "")
	for _, resource := range []struct{ path, contentType string }{
		{"/", "text/html; charset=utf-8"},
		{"/.well-known/home.css", "text/css; charset=utf-8"},
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete} {
			t.Run(method+resource.path, func(t *testing.T) {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(method, resource.path, nil))
				for name, want := range map[string]string{
					"Content-Security-Policy": "default-src 'none'; style-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'",
					"Cache-Control":           "public, max-age=300",
					"X-Content-Type-Options":  "nosniff",
					"X-Frame-Options":         "DENY",
					"Referrer-Policy":         "no-referrer",
				} {
					if got := response.Header().Get(name); got != want {
						t.Errorf("%s = %q, want %q", name, got, want)
					}
				}
				if len(response.Result().Cookies()) != 0 {
					t.Error("public resource sets cookies")
				}
				if method != http.MethodGet && method != http.MethodHead {
					if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "GET, HEAD" {
						t.Errorf("method status/Allow = %d/%q", response.Code, response.Header().Get("Allow"))
					}
					return
				}
				if response.Code != http.StatusOK || response.Header().Get("Content-Type") != resource.contentType {
					t.Errorf("resource status/content-type = %d/%q", response.Code, response.Header().Get("Content-Type"))
				}
				if method == http.MethodHead && response.Body.Len() != 0 {
					t.Error("HEAD includes body")
				}
				if method == http.MethodGet && resource.path != "/" {
					css := response.Body.String()
					for _, required := range []string{":focus-visible", "min-height: 44px", "min-width: 44px"} {
						if !strings.Contains(css, required) {
							t.Errorf("CSS missing %q", required)
						}
					}
					for _, forbidden := range []string{"@import", "url("} {
						if strings.Contains(css, forbidden) {
							t.Errorf("CSS contains %q", forbidden)
						}
					}
				}
			})
		}
	}
}

func TestPublicHomepageDoesNotShadowOtherRoutes(t *testing.T) {
	database := testDatabase(t)
	for _, slug := range []string{"home", "assets", "home.css", "admin"} {
		if err := database.InsertRedirect(slug, "https://example.com/"+slug); err != nil {
			t.Fatal(err)
		}
	}
	router := routes.NewRouterWithAdmin(database, testManagementToken, "", routes.AdminConfig{Origin: "https://gottem.link", SessionSecret: testSessionSecret, SecureCookies: true})
	for _, test := range []struct {
		path     string
		status   int
		location string
	}{
		{"/home", 302, "https://example.com/home"}, {"/assets", 302, "https://example.com/assets"}, {"/home.css", 302, "https://example.com/home.css"}, {"/admin", 302, "https://example.com/admin"},
		{"/missing", 404, ""}, {"/missing/nested", 404, ""}, {"/.well-known/missing", 404, ""}, {"/.well-known/home.css/extra", 404, ""},
		{"/.well-known/healthz", 200, ""}, {"/.well-known/readyz", 200, ""}, {"/admin/", 200, ""}, {"/admin/assets/admin.css", 200, ""}, {"/api/v1/redirects", 401, ""},
	} {
		t.Run(test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
			if response.Code != test.status || response.Header().Get("Location") != test.location {
				t.Errorf("status/location = %d/%q, want %d/%q", response.Code, response.Header().Get("Location"), test.status, test.location)
			}
			if strings.Contains(response.Body.String(), "Open the complete address you were sent") {
				t.Error("non-root route served homepage")
			}
		})
	}
}
