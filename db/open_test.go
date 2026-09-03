package db_test

import (
	"context"
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

func TestReadyRequiresRedirectsTable(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(database.Close)

	if err := database.Ready(context.Background()); err == nil {
		t.Fatal("Ready returned nil without the redirects table")
	}
}

func TestReadyRejectsOutdatedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gottem.db")
	database, err := db.GetDB(path)
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	if _, err := database.Exec(`PRAGMA user_version = 1`); err != nil {
		t.Fatalf("set outdated schema version: %v", err)
	}
	if _, err := database.Exec("ALTER TABLE redirects DROP COLUMN disabled_at"); err != nil {
		t.Fatalf("restore version-one schema: %v", err)
	}

	if err := database.Ready(context.Background()); err == nil {
		t.Fatal("Ready returned nil for an outdated schema")
	}
	database.Close()
}

func TestReadyAcceptsInitializedDatabase(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(database.Close)

	if err := database.Ready(context.Background()); err != nil {
		t.Fatalf("Ready returned an error: %v", err)
	}
}
