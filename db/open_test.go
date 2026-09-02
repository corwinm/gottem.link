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

func TestOpenDoesNotCreateOrMigrateSchema(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(database.Close)

	var redirectsExist bool
	if err := database.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM sqlite_master
			WHERE type = 'table' AND name = 'redirects'
		)
	`).Scan(&redirectsExist); err != nil {
		t.Fatalf("inspect schema: %v", err)
	}
	if redirectsExist {
		t.Fatal("Open created the redirects table; want a non-writing open")
	}
}
