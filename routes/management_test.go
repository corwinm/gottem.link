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

func TestManagementAPIRequiresAuthentication(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(database.Close)
	router := routes.NewRouter(database, testManagementToken)

	for _, path := range []string{"/api/v1/redirects", "/api/v1/imports"} {
		for name, token := range map[string]string{"missing": "", "wrong": "wrong-token"} {
			t.Run(path+" "+name, func(t *testing.T) {
				response := managementRequest(t, router, http.MethodPost, path, `{}`, token)
				if response.Code != http.StatusUnauthorized {
					t.Fatalf("status = %d, want 401", response.Code)
				}
			})
		}
	}

	for _, path := range []string{"/api/v1/redirects", "/api/v1/imports"} {
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
		"unsupported version": `{"version":2,"redirects":[]}`,
		"duplicate and fields": `{"version":1,"redirects":[` +
			`{"slug":"bad slug","url":"` + secret + `","disabled":false},` +
			`{"slug":"DUP","url":"bad","disabled":false},` +
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
