package db_test

import (
	"path/filepath"
	"testing"

	"corwinm/gottem.link/db"
)

func TestOpenDefersConnectionValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "gottem.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("open database handle: %v", err)
	}
	database.Close()
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
