package routes_test

import (
	"corwinm/gottem.link/db"
	"corwinm/gottem.link/routes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestRouterRedirectsKnownSlug(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(database.Close)

	const target = "https://example.com/destination"
	if err := database.InsertRedirect("known", target); err != nil {
		t.Fatalf("insert redirect: %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/KnOwN", nil)
	routes.NewRouter(database).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusFound)
	}
	if location := recorder.Header().Get("Location"); location != target {
		t.Errorf("Location = %q, want %q", location, target)
	}
}

func TestRouterReturnsNotFoundForUnknownSlug(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(database.Close)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	routes.NewRouter(database).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestRouterReturnsInternalServerErrorWhenDatabaseFails(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	database.Close()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/known", nil)
	routes.NewRouter(database).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
}

func TestRouterHealthDoesNotRequireDatabase(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "missing", "gottem.db"))
	if err != nil {
		t.Fatalf("open database handle: %v", err)
	}
	t.Cleanup(database.Close)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/.well-known/healthz", nil)
	routes.NewRouter(database).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestOperationalRoutesDoNotShadowRedirectSlugs(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(database.Close)

	for _, slug := range []string{"healthz", "readyz"} {
		const target = "https://example.com/operational-slug"
		if err := database.InsertRedirect(slug, target); err != nil {
			t.Fatalf("insert %s redirect: %v", slug, err)
		}

		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/"+slug, nil)
		routes.NewRouter(database).ServeHTTP(recorder, request)

		if recorder.Code != http.StatusFound {
			t.Errorf("%s status = %d, want %d", slug, recorder.Code, http.StatusFound)
		}
		if location := recorder.Header().Get("Location"); location != target {
			t.Errorf("%s Location = %q, want %q", slug, location, target)
		}
	}
}

func TestRouterReadinessChecksDatabase(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(database.Close)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/.well-known/readyz", nil)
	routes.NewRouter(database).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestRouterReadinessFailsWhenDatabaseFails(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "missing", "gottem.db"))
	if err != nil {
		t.Fatalf("open database handle: %v", err)
	}
	t.Cleanup(database.Close)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/.well-known/readyz", nil)
	routes.NewRouter(database).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}
