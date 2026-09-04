package routes

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"corwinm/gottem.link/db"
)

const generatedSlugTestToken = "test-management-token"

type managementError struct {
	Error string `json:"error"`
	Field string `json:"field,omitempty"`
}

func TestCreateRedirectGeneratesOmittedSlug(t *testing.T) {
	database := testDatabase(t)
	calls := 0
	router := newRouter(database, generatedSlugTestToken, func() (string, error) {
		calls++
		return "aaaaaaa", nil
	})

	response := routeRequest(router, http.MethodPost, "/api/v1/redirects", `{"url":"https://example.com/generated"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status/body = %d/%s, want 201", response.Code, response.Body.String())
	}
	if calls != 1 {
		t.Fatalf("generator calls = %d, want 1", calls)
	}
	if location := response.Header().Get("Location"); location != "/api/v1/redirects/aaaaaaa" {
		t.Fatalf("Location = %q, want management item URL", location)
	}
	var created db.Redirect
	decodeRouteJSON(t, response, &created)
	if created.Slug != "aaaaaaa" || created.URL != "https://example.com/generated" {
		t.Fatalf("created redirect = %#v", created)
	}

	public := routeRequest(router, http.MethodGet, "/aaaaaaa", "")
	if public.Code != http.StatusFound || public.Header().Get("Location") != created.URL {
		t.Fatalf("public status/location = %d/%q, want 302/%q", public.Code, public.Header().Get("Location"), created.URL)
	}
}

func TestCreateRedirectGeneratesNullSlug(t *testing.T) {
	database := testDatabase(t)
	router := newRouter(database, generatedSlugTestToken, func() (string, error) { return "aaaaaaa", nil })

	response := routeRequest(router, http.MethodPost, "/api/v1/redirects", `{"slug":null,"url":"https://example.com/null"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status/body = %d/%s, want 201", response.Code, response.Body.String())
	}
}

func TestCreateRedirectCanonicalizesCustomSlugWithoutGenerating(t *testing.T) {
	database := testDatabase(t)
	calls := 0
	router := newRouter(database, generatedSlugTestToken, func() (string, error) {
		calls++
		return "unusedaa", nil
	})

	response := routeRequest(router, http.MethodPost, "/api/v1/redirects", `{"slug":"MiXeD-Case","url":"https://example.com/custom"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status/body = %d/%s, want 201", response.Code, response.Body.String())
	}
	if calls != 0 {
		t.Fatalf("generator calls = %d, want 0", calls)
	}
	var created db.Redirect
	decodeRouteJSON(t, response, &created)
	if created.Slug != "mixed-case" {
		t.Fatalf("created slug = %q, want mixed-case", created.Slug)
	}
}

func TestCreateRedirectCustomConflictDoesNotRetryOrGenerate(t *testing.T) {
	database := testDatabase(t)
	if _, err := database.CreateRedirect("known", "https://example.com/first"); err != nil {
		t.Fatalf("seed redirect: %v", err)
	}
	calls := 0
	router := newRouter(database, generatedSlugTestToken, func() (string, error) {
		calls++
		return "unusedaa", nil
	})

	response := routeRequest(router, http.MethodPost, "/api/v1/redirects", `{"slug":"KNOWN","url":"https://example.com/second"}`)
	assertRouteError(t, response, http.StatusConflict, "slug already exists", "slug")
	if calls != 0 {
		t.Fatalf("generator calls = %d, want 0", calls)
	}
	redirects, err := database.ListRedirects()
	if err != nil {
		t.Fatalf("list redirects: %v", err)
	}
	if len(redirects) != 1 || redirects[0].URL != "https://example.com/first" {
		t.Fatalf("redirects after conflict = %#v", redirects)
	}
}

func TestCreateRedirectRetriesGeneratedSlugConflict(t *testing.T) {
	database := testDatabase(t)
	if _, err := database.CreateRedirect("aaaaaaa", "https://example.com/existing"); err != nil {
		t.Fatalf("seed redirect: %v", err)
	}
	generator, calls := scriptedGenerator("aaaaaaa", "bbbbbbb")
	router := newRouter(database, generatedSlugTestToken, generator)

	response := routeRequest(router, http.MethodPost, "/api/v1/redirects", `{"url":"https://example.com/new"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status/body = %d/%s, want 201", response.Code, response.Body.String())
	}
	if *calls != 2 {
		t.Fatalf("generator calls = %d, want 2", *calls)
	}
	var created db.Redirect
	decodeRouteJSON(t, response, &created)
	if created.Slug != "bbbbbbb" {
		t.Fatalf("created slug = %q, want bbbbbbb", created.Slug)
	}
}

func TestCreateRedirectStopsAfterFiveGeneratedConflicts(t *testing.T) {
	database := testDatabase(t)
	candidates := []string{"aaaaaaa", "bbbbbbb", "ccccccc", "ddddddd", "eeeeeee"}
	for _, slug := range candidates {
		if _, err := database.CreateRedirect(slug, "https://example.com/existing"); err != nil {
			t.Fatalf("seed %s: %v", slug, err)
		}
	}
	generator, calls := scriptedGenerator(candidates...)
	router := newRouter(database, generatedSlugTestToken, generator)

	response := routeRequest(router, http.MethodPost, "/api/v1/redirects", `{"url":"https://example.com/new"}`)
	assertRouteError(t, response, http.StatusConflict, "unable to generate unique slug", "slug")
	if *calls != 5 {
		t.Fatalf("generator calls = %d, want 5", *calls)
	}
}

func TestCreateRedirectGeneratorFailureDoesNotInsert(t *testing.T) {
	database := testDatabase(t)
	calls := 0
	router := newRouter(database, generatedSlugTestToken, func() (string, error) {
		calls++
		return "", errors.New("random source failed")
	})

	response := routeRequest(router, http.MethodPost, "/api/v1/redirects", `{"url":"https://example.com/new"}`)
	assertRouteError(t, response, http.StatusInternalServerError, "internal server error", "")
	if calls != 1 {
		t.Fatalf("generator calls = %d, want 1", calls)
	}
	assertRedirectCount(t, database, 0)
}

func TestCreateRedirectRejectsInvalidGeneratedCandidate(t *testing.T) {
	database := testDatabase(t)
	router := newRouter(database, generatedSlugTestToken, func() (string, error) { return "bad_slug", nil })

	response := routeRequest(router, http.MethodPost, "/api/v1/redirects", `{"url":"https://example.com/new"}`)
	assertRouteError(t, response, http.StatusInternalServerError, "internal server error", "")
	assertRedirectCount(t, database, 0)
}

func TestCreateRedirectStopsOnGeneratedDatabaseFailure(t *testing.T) {
	database := testDatabase(t)
	database.Close()
	calls := 0
	router := newRouter(database, generatedSlugTestToken, func() (string, error) {
		calls++
		return "aaaaaaa", nil
	})

	response := routeRequest(router, http.MethodPost, "/api/v1/redirects", `{"url":"https://example.com/new"}`)
	assertRouteError(t, response, http.StatusInternalServerError, "internal server error", "")
	if calls != 1 {
		t.Fatalf("generator calls = %d, want 1", calls)
	}
}

func TestCreateRedirectValidatesURLBeforeGenerator(t *testing.T) {
	tests := []struct {
		name string
		url  string
		slug *string
	}{
		{name: "missing", url: ""},
		{name: "whitespace", url: "   "},
		{name: "relative", url: "/relative"},
		{name: "unsupported scheme", url: "ftp://example.com/file"},
		{name: "credentials", url: "https://user@example.com/path"},
		{name: "invalid host", url: "https://bad_host/path"},
		{name: "before custom slug", url: "/relative", slug: stringPointer("bad_slug")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := testDatabase(t)
			calls := 0
			router := newRouter(database, generatedSlugTestToken, func() (string, error) {
				calls++
				return "aaaaaaa", nil
			})
			body, err := json.Marshal(struct {
				Slug *string `json:"slug,omitempty"`
				URL  string  `json:"url"`
			}{Slug: test.slug, URL: test.url})
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}

			response := routeRequest(router, http.MethodPost, "/api/v1/redirects", string(body))
			assertRouteError(t, response, http.StatusBadRequest, "invalid URL", "url")
			if calls != 0 {
				t.Fatalf("generator calls = %d, want 0", calls)
			}
			assertRedirectCount(t, database, 0)
		})
	}
}

func TestCreateRedirectMalformedJSONHasGenericError(t *testing.T) {
	database := testDatabase(t)
	router := newRouter(database, generatedSlugTestToken, func() (string, error) { return "aaaaaaa", nil })

	response := routeRequest(router, http.MethodPost, "/api/v1/redirects", `{`)
	assertRouteError(t, response, http.StatusBadRequest, "invalid request", "")
}

func TestCreateRedirectRejectsInvalidCustomSlugs(t *testing.T) {
	tests := []string{"", " ", "bad_slug", "-leading", "trailing-", "two--hyphens", "api", ".well-known", ".WELL-KNOWN-healthz"}
	for _, slug := range tests {
		t.Run(slug, func(t *testing.T) {
			database := testDatabase(t)
			calls := 0
			router := newRouter(database, generatedSlugTestToken, func() (string, error) {
				calls++
				return "aaaaaaa", nil
			})
			body, err := json.Marshal(map[string]string{"slug": slug, "url": "https://example.com"})
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}

			response := routeRequest(router, http.MethodPost, "/api/v1/redirects", string(body))
			assertRouteError(t, response, http.StatusBadRequest, "invalid slug", "slug")
			if calls != 0 {
				t.Fatalf("generator calls = %d, want 0", calls)
			}
		})
	}
}

func TestUpdateRedirectValidatesURL(t *testing.T) {
	database := testDatabase(t)
	if _, err := database.CreateRedirect("known", "https://example.com/original"); err != nil {
		t.Fatalf("seed redirect: %v", err)
	}
	router := newRouter(database, generatedSlugTestToken, func() (string, error) { return "unusedaa", nil })

	response := routeRequest(router, http.MethodPut, "/api/v1/redirects/known", `{"url":"javascript:alert(1)"}`)
	assertRouteError(t, response, http.StatusBadRequest, "invalid URL", "url")
	redirect, err := database.GetRedirect("known")
	if err != nil {
		t.Fatalf("get redirect: %v", err)
	}
	if redirect.URL != "https://example.com/original" {
		t.Fatalf("URL after rejected update = %q", redirect.URL)
	}
}

func testDatabase(t *testing.T) *db.DbWrapper {
	t.Helper()
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(database.Close)
	return database
}

func scriptedGenerator(values ...string) (func() (string, error), *int) {
	calls := 0
	return func() (string, error) {
		value := values[calls]
		calls++
		return value, nil
	}, &calls
}

func stringPointer(value string) *string {
	return &value
}

func routeRequest(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+generatedSlugTestToken)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func assertRouteError(t *testing.T, response *httptest.ResponseRecorder, status int, message, field string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status/body = %d/%s, want %d", response.Code, response.Body.String(), status)
	}
	var payload managementError
	decodeRouteJSON(t, response, &payload)
	if payload.Error != message || payload.Field != field {
		t.Fatalf("error payload = %#v, want error=%q field=%q", payload, message, field)
	}
}

func decodeRouteJSON(t *testing.T, response *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), destination); err != nil {
		t.Fatalf("decode JSON %q: %v", response.Body.String(), err)
	}
}

func assertRedirectCount(t *testing.T, database *db.DbWrapper, want int) {
	t.Helper()
	redirects, err := database.ListRedirects()
	if err != nil {
		t.Fatalf("list redirects: %v", err)
	}
	if len(redirects) != want {
		t.Fatalf("redirect count = %d, want %d", len(redirects), want)
	}
}
