package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"corwinm/gottem.link/backup"
	"corwinm/gottem.link/db"
	"corwinm/gottem.link/routes"
)

func TestManagementCLIExpirationLifecycleAgainstRealRouter(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	server := httptest.NewServer(routes.NewRouter(database, testToken))
	t.Cleanup(server.Close)
	baseArgs := []string{"--base-url", server.URL, "--json"}

	code, created, stderr := runCLI(t, append(baseArgs, "create", "--slug", "timed", "--expires-at", "2999-01-01T00:00:00Z", "https://example.com"), server.Client(), nil)
	if code != 0 || stderr != "" || !strings.Contains(created, `"expires_at":"2999-01-01T00:00:00Z"`) {
		t.Fatalf("create code/stdout/stderr = %d/%q/%q", code, created, stderr)
	}
	code, expired, stderr := runCLI(t, append(baseArgs, "expire", "timed", "2000-01-01T00:00:00Z"), server.Client(), nil)
	if code != 0 || stderr != "" || !strings.Contains(expired, `"expires_at":"2000-01-01T00:00:00Z"`) {
		t.Fatalf("expire code/stdout/stderr = %d/%q/%q", code, expired, stderr)
	}
	code, cleared, stderr := runCLI(t, append(baseArgs, "unexpire", "timed"), server.Client(), nil)
	if code != 0 || stderr != "" || !strings.Contains(cleared, `"expires_at":null`) {
		t.Fatalf("unexpire code/stdout/stderr = %d/%q/%q", code, cleared, stderr)
	}
	code, inspected, stderr := runCLI(t, append(baseArgs, "get", "timed"), server.Client(), nil)
	if code != 0 || stderr != "" || !strings.Contains(inspected, `"destination_updated_at":`) || !strings.Contains(inspected, `"expires_at":null`) {
		t.Fatalf("get code/stdout/stderr = %d/%q/%q", code, inspected, stderr)
	}
}

func TestExportImportLifecycleBetweenRealDatabases(t *testing.T) {
	sourceDB, err := db.GetDB(filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sourceDB.Close)
	if _, err := sourceDB.CreateRedirect("active", "https://example.com/active"); err != nil {
		t.Fatal(err)
	}
	if _, err := sourceDB.CreateRedirect("disabled", "https://example.com/disabled"); err != nil {
		t.Fatal(err)
	}
	if _, err := sourceDB.DisableRedirect("disabled"); err != nil {
		t.Fatal(err)
	}
	sourceServer := httptest.NewServer(routes.NewRouter(sourceDB, testToken))
	t.Cleanup(sourceServer.Close)

	code, exported, stderr := runCLI(t, []string{"--base-url", sourceServer.URL, "export"}, sourceServer.Client(), nil)
	if code != 0 || stderr != "" {
		t.Fatalf("export code/stderr = %d/%q", code, stderr)
	}
	var sourceEnvelope backup.Envelope
	if err := json.Unmarshal([]byte(exported), &sourceEnvelope); err != nil {
		t.Fatalf("decode source export: %v", err)
	}
	exportPath := filepath.Join(t.TempDir(), "export.json")
	if err := os.WriteFile(exportPath, []byte(exported), 0o600); err != nil {
		t.Fatal(err)
	}

	destinationDB, err := db.GetDB(filepath.Join(t.TempDir(), "destination.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(destinationDB.Close)
	destinationRouter := routes.NewRouter(destinationDB, testToken)
	destinationServer := httptest.NewServer(destinationRouter)
	t.Cleanup(destinationServer.Close)
	baseArgs := []string{"--base-url", destinationServer.URL}

	code, _, stderr = runCLI(t, append(baseArgs, "import", exportPath), destinationServer.Client(), nil)
	if code != 0 || stderr != "" {
		t.Fatalf("dry-run code/stderr = %d/%q", code, stderr)
	}
	if listed, err := destinationDB.ListRedirects(); err != nil || len(listed) != 0 {
		t.Fatalf("destination after dry run = %#v, %v", listed, err)
	}
	code, _, stderr = runCLI(t, append(baseArgs, "import", "--apply", exportPath), destinationServer.Client(), nil)
	if code != 0 || stderr != "" {
		t.Fatalf("apply code/stderr = %d/%q", code, stderr)
	}

	active := httptest.NewRecorder()
	destinationRouter.ServeHTTP(active, httptest.NewRequest(http.MethodGet, "/active", nil))
	if active.Code != http.StatusFound || active.Header().Get("Location") != "https://example.com/active" {
		t.Fatalf("active resolution = %d/%q", active.Code, active.Header().Get("Location"))
	}
	disabled := httptest.NewRecorder()
	destinationRouter.ServeHTTP(disabled, httptest.NewRequest(http.MethodGet, "/disabled", nil))
	if disabled.Code != http.StatusNotFound {
		t.Fatalf("disabled resolution = %d, want 404", disabled.Code)
	}

	code, reexported, stderr := runCLI(t, append(baseArgs, "export"), destinationServer.Client(), nil)
	if code != 0 || stderr != "" {
		t.Fatalf("re-export code/stderr = %d/%q", code, stderr)
	}
	var destinationEnvelope backup.Envelope
	if err := json.Unmarshal([]byte(reexported), &destinationEnvelope); err != nil {
		t.Fatalf("decode destination export: %v", err)
	}
	if !reflect.DeepEqual(destinationEnvelope, sourceEnvelope) {
		t.Fatalf("re-export = %#v, want %#v", destinationEnvelope, sourceEnvelope)
	}
}

func TestExportImportRestoresMigratedLegacyValuesExactly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`CREATE TABLE redirects (id INTEGER PRIMARY KEY, slug TEXT, url TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO redirects (slug, url) VALUES (?, ?), (?, ?)`, "Legacy_Slug", "/Preserve/%2F?q=A%20B", "OFF_Ä", "mailto:legacy@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	sourceDB, err := db.GetDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sourceDB.Close)
	if _, err := sourceDB.DisableRedirect("OFF_Ä"); err != nil {
		t.Fatal(err)
	}
	sourceServer := httptest.NewServer(routes.NewRouter(sourceDB, testToken))
	t.Cleanup(sourceServer.Close)

	code, exported, stderr := runCLI(t, []string{"--base-url", sourceServer.URL, "export"}, sourceServer.Client(), nil)
	if code != 0 || stderr != "" {
		t.Fatalf("export code/stderr = %d/%q", code, stderr)
	}
	exportPath := filepath.Join(t.TempDir(), "export.json")
	if err := os.WriteFile(exportPath, []byte(exported), 0o600); err != nil {
		t.Fatal(err)
	}

	destinationDB, err := db.GetDB(filepath.Join(t.TempDir(), "destination.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(destinationDB.Close)
	destinationRouter := routes.NewRouter(destinationDB, testToken)
	destinationServer := httptest.NewServer(destinationRouter)
	t.Cleanup(destinationServer.Close)
	code, _, stderr = runCLI(t, []string{"--base-url", destinationServer.URL, "import", "--apply", exportPath}, destinationServer.Client(), nil)
	if code != 0 || stderr != "" {
		t.Fatalf("import code/stderr = %d/%q", code, stderr)
	}

	public := httptest.NewRecorder()
	destinationRouter.ServeHTTP(public, httptest.NewRequest(http.MethodGet, "/legacy_slug", nil))
	if public.Code != http.StatusFound || public.Header().Get("Location") != "/Preserve/%2F?q=A%20B" {
		t.Fatalf("public behavior = %d/%q", public.Code, public.Header().Get("Location"))
	}
	disabled := httptest.NewRecorder()
	destinationRouter.ServeHTTP(disabled, httptest.NewRequest(http.MethodGet, "/off_Ä", nil))
	if disabled.Code != http.StatusNotFound {
		t.Fatalf("disabled behavior = %d, want 404", disabled.Code)
	}

	code, reexported, stderr := runCLI(t, []string{"--base-url", destinationServer.URL, "export"}, destinationServer.Client(), nil)
	if code != 0 || stderr != "" || reexported != exported {
		t.Fatalf("re-export code/stdout/stderr = %d/%q/%q, want original", code, reexported, stderr)
	}
}

func TestCompactExportSucceedsWhenListResponseExceedsClientLimit(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if _, err := database.Exec(`WITH RECURSIVE numbers(n) AS (VALUES(1) UNION ALL SELECT n + 1 FROM numbers WHERE n < 9000) INSERT INTO redirects (slug, url) SELECT printf('s%05d', n), 'x' FROM numbers`); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(routes.NewRouter(database, testToken))
	t.Cleanup(server.Close)

	listCode, _, listStderr := runCLI(t, []string{"--base-url", server.URL, "list"}, server.Client(), nil)
	if listCode != 1 || !strings.Contains(listStderr, "server response exceeds 1 MiB") {
		t.Fatalf("list code/stderr = %d/%q", listCode, listStderr)
	}
	exportCode, exported, exportStderr := runCLI(t, []string{"--base-url", server.URL, "export"}, server.Client(), nil)
	if exportCode != 0 || exportStderr != "" {
		t.Fatalf("export code/stderr = %d/%q", exportCode, exportStderr)
	}
	envelope, err := backup.Decode(strings.NewReader(exported))
	if err != nil || len(envelope.Redirects) != 9000 {
		t.Fatalf("decode compact export = %d redirects, %v", len(envelope.Redirects), err)
	}
}

func TestManagementCLILifecycleAgainstRealRouter(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(database.Close)

	server := httptest.NewServer(routes.NewRouter(database, testToken))
	t.Cleanup(server.Close)
	baseArgs := []string{"--base-url", server.URL, "--json"}

	code, createdOut, stderr := runCLI(t, append(baseArgs, "create", "--slug", "known", "https://example.com/one"), server.Client(), nil)
	if code != 0 || stderr != "" {
		t.Fatalf("create code/stderr = %d/%q", code, stderr)
	}
	var created redirect
	if err := json.Unmarshal([]byte(createdOut), &created); err != nil || created.Slug != "known" {
		t.Fatalf("created output = %q, redirect=%#v, err=%v", createdOut, created, err)
	}

	code, listOut, stderr := runCLI(t, append(baseArgs, "list"), server.Client(), nil)
	if code != 0 || stderr != "" {
		t.Fatalf("list code/stderr = %d/%q", code, stderr)
	}
	var listed []redirect
	if err := json.Unmarshal([]byte(listOut), &listed); err != nil || len(listed) != 1 || listed[0].Slug != "known" {
		t.Fatalf("listed output = %q, redirects=%#v, err=%v", listOut, listed, err)
	}

	code, _, stderr = runCLI(t, append(baseArgs, "update", "known", "https://example.com/two"), server.Client(), nil)
	if code != 0 || stderr != "" {
		t.Fatalf("update code/stderr = %d/%q", code, stderr)
	}
	code, getOut, stderr := runCLI(t, append(baseArgs, "get", "known"), server.Client(), nil)
	if code != 0 || stderr != "" {
		t.Fatalf("get code/stderr = %d/%q", code, stderr)
	}
	var got redirect
	if err := json.Unmarshal([]byte(getOut), &got); err != nil || got.URL != "https://example.com/two" {
		t.Fatalf("get output = %q, redirect=%#v, err=%v", getOut, got, err)
	}

	code, disabledOut, stderr := runCLI(t, append(baseArgs, "disable", "known"), server.Client(), nil)
	if code != 0 || stderr != "" {
		t.Fatalf("disable code/stderr = %d/%q", code, stderr)
	}
	if err := json.Unmarshal([]byte(disabledOut), &got); err != nil || got.DisabledAt == nil {
		t.Fatalf("disabled output = %q, redirect=%#v, err=%v", disabledOut, got, err)
	}

	publicResponse, err := server.Client().Get(server.URL + "/known")
	if err != nil {
		t.Fatalf("request disabled public redirect: %v", err)
	}
	_ = publicResponse.Body.Close()
	if publicResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("disabled public status = %d, want 404", publicResponse.StatusCode)
	}

	code, deletedOut, stderr := runCLI(t, append(baseArgs, "delete", "--force", "known"), server.Client(), nil)
	if code != 0 || stderr != "" {
		t.Fatalf("delete code/stderr = %d/%q", code, stderr)
	}
	var deleted map[string]any
	if err := json.Unmarshal([]byte(deletedOut), &deleted); err != nil || deleted["deleted"] != true || deleted["slug"] != "known" {
		t.Fatalf("deleted output = %q, payload=%#v, err=%v", deletedOut, deleted, err)
	}

	code, _, _ = runCLI(t, append(baseArgs, "get", "known"), server.Client(), nil)
	if code != 1 {
		t.Fatalf("get after delete code = %d, want 1", code)
	}
}
