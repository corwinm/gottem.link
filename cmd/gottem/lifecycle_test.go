package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"corwinm/gottem.link/backup"
	"corwinm/gottem.link/db"
	"corwinm/gottem.link/routes"
)

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
