package db_test

import (
	"path/filepath"
	"testing"

	"corwinm/gottem.link/db"
)

func TestOpenRejectsInaccessibleDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "gottem.db")
	if database, err := db.Open(path); err == nil {
		database.Close()
		t.Fatal("Open succeeded for a database in a missing directory")
	}
}

func TestOpenRejectsMissingRedirectsSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gottem.db")
	if database, err := db.Open(path); err == nil {
		database.Close()
		t.Fatal("Open succeeded without a redirects table")
	}
}

func TestOpenDoesNotCreateOrMigrateSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gottem.db")
	initialized, err := db.GetDB(path)
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	initialized.Close()

	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(database.Close)

	var version int
	if err := database.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("query schema version: %v", err)
	}
	if version != 0 {
		t.Fatalf("schema version = %d, want unchanged version 0", version)
	}
}
