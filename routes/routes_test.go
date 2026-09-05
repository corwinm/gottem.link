package routes_test

import (
	"context"
	"corwinm/gottem.link/db"
	"corwinm/gottem.link/routes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
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
	routes.NewRouter(database, "").ServeHTTP(recorder, request)

	if recorder.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusFound)
	}
	if location := recorder.Header().Get("Location"); location != target {
		t.Errorf("Location = %q, want %q", location, target)
	}
}

func TestRouterTracksOnlySuccessfulPublicRedirects(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	active, _ := database.CreateRedirect("active", "https://example.com/active")
	disabled, _ := database.CreateRedirect("disabled", "https://example.com/disabled")
	_, _ = database.DisableRedirect(disabled.Slug)
	past := "2000-01-01T00:00:00Z"
	expired, _ := database.CreateRedirectWithExpiration("expired", "https://example.com/expired", &past)
	writer := db.NewAccessWriter(database, 16, nil)
	router := routes.NewRouterWithStats(database, "", writer)

	for _, path := range []string{"/", "/missing", "/disabled", "/expired", "/.well-known/healthz", "/.well-known/readyz", "/admin", "/api/v1/redirects"} {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/ACTIVE", nil))
	if response.Code != http.StatusFound {
		t.Fatalf("active status = %d", response.Code)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	for _, expected := range []struct{ id, count int64 }{{active.ID, 1}, {disabled.ID, 0}, {expired.ID, 0}} {
		var count int64
		var accessedAt any
		if err := database.QueryRow(`SELECT click_count, last_accessed_at FROM redirects WHERE id = ?`, expected.id).Scan(&count, &accessedAt); err != nil {
			t.Fatal(err)
		}
		if count != expected.count || (expected.count == 0 && accessedAt != nil) || (expected.count == 1 && accessedAt == nil) {
			t.Fatalf("id %d stats = %d/%v, want %d", expected.id, count, accessedAt, expected.count)
		}
	}
}

func TestRouterRegistersAuthenticatedInternalAccessWrite(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	redirect, err := database.CreateRedirect("active", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	router := routes.NewRouterWithAdminStats(database, "shared-token", "", routes.AdminConfig{}, nil, database)
	request := httptest.NewRequest(http.MethodPost, "/.internal/accesses", strings.NewReader(`{"redirect_id":`+strconv.FormatInt(redirect.ID, 10)+`,"accessed_at":"2026-01-02T03:04:05Z"}`))
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("Authorization", "Bearer shared-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
	}
	stored, err := database.GetRedirect("active")
	if err != nil || stored.ClickCount != 1 {
		t.Fatalf("stored = %#v, err = %v", stored, err)
	}
}

func TestRouterDisablesTypedNilStatsDependencies(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if _, err := database.CreateRedirect("known", "https://example.com"); err != nil {
		t.Fatal(err)
	}
	var writer *db.AccessWriter
	var store *db.DbWrapper
	router := routes.NewRouterWithAdminStats(database, "", "", routes.AdminConfig{}, writer, store)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/known", nil))
	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", response.Code)
	}
}

func TestRedirectResponseDoesNotWaitForStatsStorage(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if _, err := database.CreateRedirect("active", "https://example.com"); err != nil {
		t.Fatal(err)
	}
	store := &slowRouteAccessStore{release: make(chan struct{})}
	writer := db.NewAccessWriter(store, 1, nil)
	router := routes.NewRouterWithStats(database, "", writer)
	started := time.Now()
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/active", nil))
	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
		t.Fatalf("redirect waited for stats storage: %s", elapsed)
	}
	close(store.release)
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type slowRouteAccessStore struct{ release chan struct{} }

func (store *slowRouteAccessStore) RecordRedirectAccess(_ context.Context, _ int64, _ time.Time) error {
	<-store.release
	return nil
}

func TestRouterReturnsNotFoundForUnknownSlug(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(database.Close)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	routes.NewRouter(database, "").ServeHTTP(recorder, request)

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
	routes.NewRouter(database, "").ServeHTTP(recorder, request)

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
	routes.NewRouter(database, "").ServeHTTP(recorder, request)

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
		routes.NewRouter(database, "").ServeHTTP(recorder, request)

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
	routes.NewRouter(database, "").ServeHTTP(recorder, request)

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
	routes.NewRouter(database, "").ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}
