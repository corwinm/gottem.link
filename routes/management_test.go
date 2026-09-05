package routes_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"corwinm/gottem.link/backup"
	"corwinm/gottem.link/db"
	"corwinm/gottem.link/routes"
)

const testManagementToken = "test-management-token"

func TestManagementAPILifecycle(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(database.Close)
	router := routes.NewRouter(database, testManagementToken)

	createdResponse := managementRequest(t, router, http.MethodPost, "/api/v1/redirects", `{"slug":"Known","url":"https://example.com/one"}`, testManagementToken)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create status/body = %d/%s, want 201", createdResponse.Code, createdResponse.Body.String())
	}
	if contentType := createdResponse.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("create Content-Type = %q, want application/json", contentType)
	}
	var created db.Redirect
	decodeJSON(t, createdResponse, &created)
	if created.Slug != "known" || created.URL != "https://example.com/one" {
		t.Fatalf("created redirect = %#v", created)
	}

	public := httptest.NewRecorder()
	router.ServeHTTP(public, httptest.NewRequest(http.MethodGet, "/known", nil))
	if public.Code != http.StatusFound || public.Header().Get("Location") != "https://example.com/one" {
		t.Fatalf("public redirect status/location = %d/%q", public.Code, public.Header().Get("Location"))
	}

	listedResponse := managementRequest(t, router, http.MethodGet, "/api/v1/redirects", "", testManagementToken)
	if listedResponse.Code != http.StatusOK {
		t.Fatalf("list status/body = %d/%s", listedResponse.Code, listedResponse.Body.String())
	}
	var listed []db.Redirect
	decodeJSON(t, listedResponse, &listed)
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("listed redirects = %#v", listed)
	}

	inspectedResponse := managementRequest(t, router, http.MethodGet, "/api/v1/redirects/KNOWN", "", testManagementToken)
	if inspectedResponse.Code != http.StatusOK {
		t.Fatalf("inspect status/body = %d/%s", inspectedResponse.Code, inspectedResponse.Body.String())
	}
	var inspected db.Redirect
	decodeJSON(t, inspectedResponse, &inspected)
	if inspected.ID != created.ID {
		t.Fatalf("inspected redirect = %#v", inspected)
	}

	updatedResponse := managementRequest(t, router, http.MethodPut, "/api/v1/redirects/known", `{"url":"https://example.com/two"}`, testManagementToken)
	if updatedResponse.Code != http.StatusOK {
		t.Fatalf("update status/body = %d/%s", updatedResponse.Code, updatedResponse.Body.String())
	}
	var updated db.Redirect
	decodeJSON(t, updatedResponse, &updated)
	if updated.URL != "https://example.com/two" {
		t.Fatalf("updated redirect = %#v", updated)
	}

	disabledResponse := managementRequest(t, router, http.MethodPost, "/api/v1/redirects/known/disable", "", testManagementToken)
	if disabledResponse.Code != http.StatusOK {
		t.Fatalf("disable status/body = %d/%s", disabledResponse.Code, disabledResponse.Body.String())
	}
	var disabled db.Redirect
	decodeJSON(t, disabledResponse, &disabled)
	if disabled.DisabledAt == nil {
		t.Fatalf("disabled redirect = %#v", disabled)
	}

	publicAfterDisable := httptest.NewRecorder()
	router.ServeHTTP(publicAfterDisable, httptest.NewRequest(http.MethodGet, "/known", nil))
	if publicAfterDisable.Code != http.StatusNotFound {
		t.Fatalf("disabled public redirect status = %d, want 404", publicAfterDisable.Code)
	}

	deletedResponse := managementRequest(t, router, http.MethodDelete, "/api/v1/redirects/known", "", testManagementToken)
	if deletedResponse.Code != http.StatusNoContent || deletedResponse.Body.Len() != 0 {
		t.Fatalf("delete status/body = %d/%q, want 204/empty", deletedResponse.Code, deletedResponse.Body.String())
	}

	missingResponse := managementRequest(t, router, http.MethodGet, "/api/v1/redirects/known", "", testManagementToken)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("deleted inspect status = %d, want 404", missingResponse.Code)
	}
}

func TestManagementAPIExpirationLifecycle(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	router := routes.NewRouter(database, testManagementToken)

	createdResponse := managementRequest(t, router, http.MethodPost, "/api/v1/redirects", `{"slug":"timed","url":"https://example.com/one","expires_at":"2999-01-01T00:00:00.123456789Z"}`, testManagementToken)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create status/body = %d/%s", createdResponse.Code, createdResponse.Body.String())
	}
	var created db.Redirect
	decodeJSON(t, createdResponse, &created)
	if created.ExpiresAt == nil || *created.ExpiresAt != "2999-01-01T00:00:00.123456789Z" || created.DestinationUpdatedAt == "" {
		t.Fatalf("created redirect = %#v", created)
	}
	if _, err := database.Exec(`UPDATE redirects SET destination_updated_at = '2026-01-01 00:00:00' WHERE slug = 'timed'`); err != nil {
		t.Fatal(err)
	}

	expiredResponse := managementRequest(t, router, http.MethodPut, "/api/v1/redirects/timed/expiration", `{"expires_at":"2000-01-01T00:00:00.987654321Z"}`, testManagementToken)
	if expiredResponse.Code != http.StatusOK {
		t.Fatalf("expire status/body = %d/%s", expiredResponse.Code, expiredResponse.Body.String())
	}
	var expired db.Redirect
	decodeJSON(t, expiredResponse, &expired)
	if expired.ExpiresAt == nil || *expired.ExpiresAt != "2000-01-01T00:00:00.987654321Z" || expired.DestinationUpdatedAt != "2026-01-01 00:00:00" {
		t.Fatalf("expired redirect = %#v", expired)
	}
	public := httptest.NewRecorder()
	router.ServeHTTP(public, httptest.NewRequest(http.MethodGet, "/timed", nil))
	if public.Code != http.StatusNotFound {
		t.Fatalf("expired public status = %d, want 404", public.Code)
	}
	inspected := managementRequest(t, router, http.MethodGet, "/api/v1/redirects/timed", "", testManagementToken)
	if inspected.Code != http.StatusOK {
		t.Fatalf("expired inspect status/body = %d/%s", inspected.Code, inspected.Body.String())
	}
	duplicate := managementRequest(t, router, http.MethodPost, "/api/v1/redirects", `{"slug":"TIMED","url":"https://example.com/reuse"}`, testManagementToken)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("expired slug reuse status/body = %d/%s", duplicate.Code, duplicate.Body.String())
	}

	clearedResponse := managementRequest(t, router, http.MethodPut, "/api/v1/redirects/timed/expiration", `{"expires_at":null}`, testManagementToken)
	if clearedResponse.Code != http.StatusOK {
		t.Fatalf("clear status/body = %d/%s", clearedResponse.Code, clearedResponse.Body.String())
	}
	var cleared db.Redirect
	decodeJSON(t, clearedResponse, &cleared)
	if cleared.ExpiresAt != nil || cleared.DestinationUpdatedAt != "2026-01-01 00:00:00" {
		t.Fatalf("cleared redirect = %#v", cleared)
	}
	invalid := managementRequest(t, router, http.MethodPut, "/api/v1/redirects/timed/expiration", `{"expires_at":"tomorrow"}`, testManagementToken)
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), `"field":"expires_at"`) {
		t.Fatalf("invalid expiration status/body = %d/%s", invalid.Code, invalid.Body.String())
	}
}

func TestManagementAPIRequiresAuthentication(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(database.Close)
	router := routes.NewRouter(database, testManagementToken)

	for _, path := range []string{"/api/v1/redirects", "/api/v1/imports", "/api/v1/exports"} {
		for name, token := range map[string]string{"missing": "", "wrong": "wrong-token"} {
			t.Run(path+" "+name, func(t *testing.T) {
				response := managementRequest(t, router, http.MethodPost, path, `{}`, token)
				if response.Code != http.StatusUnauthorized {
					t.Fatalf("status = %d, want 401", response.Code)
				}
			})
		}
	}

	for _, path := range []string{"/api/v1/redirects", "/api/v1/imports", "/api/v1/exports"} {
		response := managementRequest(t, routes.NewRouter(database, ""), http.MethodPost, path, `{}`, "anything")
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, response.Code)
		}
	}
}

func TestManagementAPIDisabledWithoutConfiguredToken(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(database.Close)

	response := managementRequest(t, routes.NewRouter(database, ""), http.MethodGet, "/api/v1/redirects", "", "anything")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
}

func TestManagementAPIErrors(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(database.Close)
	router := routes.NewRouter(database, testManagementToken)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{name: "malformed create", method: http.MethodPost, path: "/api/v1/redirects", body: `{`, want: http.StatusBadRequest},
		{name: "empty create", method: http.MethodPost, path: "/api/v1/redirects", body: `{}`, want: http.StatusBadRequest},
		{name: "unknown create field", method: http.MethodPost, path: "/api/v1/redirects", body: `{"slug":"one","url":"https://example.com","extra":true}`, want: http.StatusBadRequest},
		{name: "missing item", method: http.MethodGet, path: "/api/v1/redirects/missing", want: http.StatusNotFound},
		{name: "empty update", method: http.MethodPut, path: "/api/v1/redirects/missing", body: `{}`, want: http.StatusBadRequest},
		{name: "unsupported collection method", method: http.MethodDelete, path: "/api/v1/redirects", want: http.StatusMethodNotAllowed},
		{name: "unsupported item method", method: http.MethodPatch, path: "/api/v1/redirects/missing", body: `{}`, want: http.StatusMethodNotAllowed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := managementRequest(t, router, test.method, test.path, test.body, testManagementToken)
			if response.Code != test.want {
				t.Fatalf("status/body = %d/%s, want %d", response.Code, response.Body.String(), test.want)
			}
		})
	}

	first := managementRequest(t, router, http.MethodPost, "/api/v1/redirects", `{"slug":"Known","url":"https://example.com/one"}`, testManagementToken)
	if first.Code != http.StatusCreated {
		t.Fatalf("first create status = %d", first.Code)
	}
	duplicate := managementRequest(t, router, http.MethodPost, "/api/v1/redirects", `{"slug":"known","url":"https://example.com/two"}`, testManagementToken)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate create status/body = %d/%s, want 409", duplicate.Code, duplicate.Body.String())
	}
}

func TestPortableExportEndpoint(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if _, err := database.CreateRedirect("active", "https://example.com/a"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateRedirect("off", "https://example.com/o"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DisableRedirect("off"); err != nil {
		t.Fatal(err)
	}

	response := managementRequest(t, routes.NewRouter(database, testManagementToken), http.MethodGet, "/api/v1/exports", "", testManagementToken)
	if response.Code != http.StatusOK {
		t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
	}
	var envelope backup.Envelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	if envelope.Version != backup.Version || len(envelope.Redirects) != 2 || envelope.Redirects[0].DestinationUpdatedAt == "" || envelope.Redirects[1].DestinationUpdatedAt == "" {
		t.Fatalf("export = %#v", envelope)
	}
}

func TestPortableExportPreservesLifecycleFieldsAndImportsV1(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	future := "2999-01-01T00:00:00.123456789Z"
	if _, err := database.CreateRedirectWithExpiration("timed", "https://example.com/timed", &future); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE redirects SET destination_updated_at = '2029-01-02T03:04:05.987654321Z', click_count = 12, last_accessed_at = '2029-02-03T04:05:06.123456789Z' WHERE slug = 'timed'`); err != nil {
		t.Fatal(err)
	}
	router := routes.NewRouter(database, testManagementToken)
	exported := managementRequest(t, router, http.MethodGet, "/api/v1/exports", "", testManagementToken)
	want := `{"version":3,"redirects":[{"slug":"timed","url":"https://example.com/timed","disabled":false,"expires_at":"2999-01-01T00:00:00.123456789Z","destination_updated_at":"2029-01-02T03:04:05.987654321Z","click_count":12,"last_accessed_at":"2029-02-03T04:05:06.123456789Z"}]}` + "\n"
	if exported.Code != http.StatusOK || exported.Body.String() != want {
		t.Fatalf("export status/body = %d/%q, want 200/%q", exported.Code, exported.Body.String(), want)
	}

	destination, err := db.GetDB(filepath.Join(t.TempDir(), "destination.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(destination.Close)
	destinationRouter := routes.NewRouter(destination, testManagementToken)
	for _, payload := range []string{
		`{"version":1,"redirects":[{"slug":"legacy","url":"https://example.com/legacy","disabled":false}]}`,
		exported.Body.String(),
	} {
		response := managementRequest(t, destinationRouter, http.MethodPost, "/api/v1/imports", payload, testManagementToken)
		if response.Code != http.StatusOK {
			t.Fatalf("import status/body = %d/%s", response.Code, response.Body.String())
		}
	}
	timed, err := destination.GetRedirect("timed")
	if err != nil || timed.ExpiresAt == nil || *timed.ExpiresAt != future || timed.DestinationUpdatedAt != "2029-01-02T03:04:05.987654321Z" || timed.ClickCount != 12 || timed.LastAccessedAt == nil || *timed.LastAccessedAt != "2029-02-03T04:05:06.123456789Z" {
		t.Fatalf("imported timed redirect = %#v, %v", timed, err)
	}
	legacy, err := destination.GetRedirect("legacy")
	if err != nil || legacy.ExpiresAt != nil || legacy.DestinationUpdatedAt == "" {
		t.Fatalf("imported v1 redirect = %#v, %v", legacy, err)
	}
}

func TestBackupTokenCanOnlyReadPortableExport(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	const backupToken = "test-backup-token"
	router := routes.NewRouterWithBackupToken(database, testManagementToken, backupToken)

	exported := managementRequest(t, router, http.MethodGet, "/api/v1/exports", "", backupToken)
	if exported.Code != http.StatusOK {
		t.Fatalf("export status/body = %d/%s", exported.Code, exported.Body.String())
	}
	for _, request := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/api/v1/redirects"},
		{method: http.MethodPost, path: "/api/v1/imports", body: `{"version":1,"redirects":[]}`},
	} {
		response := managementRequest(t, router, request.method, request.path, request.body, backupToken)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status = %d, want 401", request.method, request.path, response.Code)
		}
	}
}

func TestPortableExportRejectsLegacyInvalidUTF8(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	invalidURL := string([]byte{'/', 0xff})
	if _, err := database.Exec(`INSERT INTO redirects (slug, url) VALUES (?, ?)`, "legacy", invalidURL); err != nil {
		t.Fatal(err)
	}

	response := managementRequest(t, routes.NewRouter(database, testManagementToken), http.MethodGet, "/api/v1/exports", "", testManagementToken)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status/body = %d/%q, want 422", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "�") {
		t.Fatalf("response silently replaced invalid UTF-8: %q", response.Body.String())
	}
}

func TestPortableImportNormalizesExpirationForSQLiteResolution(t *testing.T) {
	database := testDatabase(t)
	router := routes.NewRouter(database, testManagementToken)
	payload := `{"version":2,"redirects":[{"slug":"offset","url":"https://example.com/future","disabled":false,"expires_at":"2999-01-01T00:00:00+15:00","destination_updated_at":"2029-01-02T03:04:05+15:00"}]}`

	imported := managementRequest(t, router, http.MethodPost, "/api/v1/imports", payload, testManagementToken)
	if imported.Code != http.StatusOK {
		t.Fatalf("import status/body = %d/%s, want 200", imported.Code, imported.Body.String())
	}
	stored, err := database.GetRedirect("offset")
	if err != nil {
		t.Fatal(err)
	}
	if stored.ExpiresAt == nil || *stored.ExpiresAt != "2998-12-31T09:00:00Z" || stored.DestinationUpdatedAt != "2029-01-01T12:04:05Z" {
		t.Fatalf("stored lifecycle = expires %#v, destination updated %q", stored.ExpiresAt, stored.DestinationUpdatedAt)
	}
	public := httptest.NewRecorder()
	router.ServeHTTP(public, httptest.NewRequest(http.MethodGet, "/offset", nil))
	if public.Code != http.StatusFound || public.Header().Get("Location") != "https://example.com/future" {
		t.Fatalf("public status/location = %d/%q, want 302/future destination", public.Code, public.Header().Get("Location"))
	}
}

func TestBulkImportDryRunAndApply(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(database.Close)
	router := routes.NewRouter(database, testManagementToken)
	payload := `{"version":1,"redirects":[{"slug":"active","url":"https://example.com/a","disabled":false},{"slug":"disabled","url":"https://example.com/d","disabled":true}]}`

	dryRun := managementRequest(t, router, http.MethodPost, "/api/v1/imports?dry_run=true", payload, testManagementToken)
	if dryRun.Code != http.StatusOK || dryRun.Body.String() != "{\"total\":2,\"imported\":0}\n" {
		t.Fatalf("dry-run status/body = %d/%s", dryRun.Code, dryRun.Body.String())
	}
	listed, err := database.ListRedirects()
	if err != nil || len(listed) != 0 {
		t.Fatalf("redirects after dry run = %#v, %v", listed, err)
	}

	applied := managementRequest(t, router, http.MethodPost, "/api/v1/imports", payload, testManagementToken)
	if applied.Code != http.StatusOK || applied.Body.String() != "{\"total\":2,\"imported\":2}\n" {
		t.Fatalf("apply status/body = %d/%s", applied.Code, applied.Body.String())
	}
	active := httptest.NewRecorder()
	router.ServeHTTP(active, httptest.NewRequest(http.MethodGet, "/active", nil))
	if active.Code != http.StatusFound || active.Header().Get("Location") != "https://example.com/a" {
		t.Fatalf("active resolution = %d/%q", active.Code, active.Header().Get("Location"))
	}
	disabled := httptest.NewRecorder()
	router.ServeHTTP(disabled, httptest.NewRequest(http.MethodGet, "/disabled", nil))
	if disabled.Code != http.StatusNotFound {
		t.Fatalf("disabled resolution = %d, want 404", disabled.Code)
	}
}

func TestBulkImportRejectsAmbiguousDryRunValuesWithoutWrites(t *testing.T) {
	payload := `{"version":1,"redirects":[{"slug":"active","url":"https://example.com/a","disabled":false}]}`
	paths := []string{
		"/api/v1/imports?dry_run=tru",
		"/api/v1/imports?dry_run=True",
		"/api/v1/imports?dry_run=true&dry_run=false",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(database.Close)

			response := managementRequest(t, routes.NewRouter(database, testManagementToken), http.MethodPost, path, payload, testManagementToken)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
			}
			listed, err := database.ListRedirects()
			if err != nil || len(listed) != 0 {
				t.Fatalf("redirects after rejection = %#v, %v", listed, err)
			}
		})
	}
}

func TestBulkImportReportsAllSortedConflictsWithoutWrites(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(database.Close)
	if _, err := database.CreateRedirect("zeta", "https://example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateRedirect("alpha", "https://example.com"); err != nil {
		t.Fatal(err)
	}
	payload := `{"version":1,"redirects":[{"slug":"ZETA","url":"https://example.com/z","disabled":false},{"slug":"new","url":"https://example.com/n","disabled":false},{"slug":"ALPHA","url":"https://example.com/a","disabled":false}]}`
	response := managementRequest(t, routes.NewRouter(database, testManagementToken), http.MethodPost, "/api/v1/imports", payload, testManagementToken)
	if response.Code != http.StatusConflict {
		t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
	}
	var body struct {
		Error     string   `json:"error"`
		Conflicts []string `json:"conflicts"`
	}
	decodeJSON(t, response, &body)
	if body.Error != "slug conflicts" || !reflect.DeepEqual(body.Conflicts, []string{"alpha", "zeta"}) {
		t.Fatalf("conflict body = %#v", body)
	}
	if _, err := database.GetRedirect("new"); err == nil {
		t.Fatal("non-conflicting redirect was written")
	}
}

func TestBulkImportRejectsInvalidEnvelopesWithoutWrites(t *testing.T) {
	secret := "https://secret.example/path"
	tests := map[string]string{
		"unknown field":       `{"version":1,"redirects":[],"extra":true}`,
		"trailing JSON":       `{"version":1,"redirects":[]} {}`,
		"unsupported version": `{"version":4,"redirects":[]}`,
		"duplicate and fields": `{"version":1,"redirects":[` +
			`{"slug":"","url":"` + secret + `","disabled":false},` +
			`{"slug":"DUP","url":"","disabled":false},` +
			`{"slug":"dup","url":"https://example.com","disabled":false}]}`,
		"oversized": strings.Repeat(" ", (1<<20)+1),
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(database.Close)
			response := managementRequest(t, routes.NewRouter(database, testManagementToken), http.MethodPost, "/api/v1/imports", payload, testManagementToken)
			if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), secret) {
				t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
			}
			listed, err := database.ListRedirects()
			if err != nil || len(listed) != 0 {
				t.Fatalf("redirects after rejection = %#v, %v", listed, err)
			}
		})
	}
}

func managementRequest(t *testing.T, handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decodeJSON(t *testing.T, recorder *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), destination); err != nil {
		t.Fatalf("decode JSON %q: %v", recorder.Body.String(), err)
	}
}
