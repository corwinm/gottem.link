package db_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"corwinm/gottem.link/db"
	_ "github.com/mattn/go-sqlite3"
)

func TestGetDBCreatesCurrentSchema(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(database.Close)

	var version int
	if err := database.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("query schema version: %v", err)
	}
	if version != 1 {
		t.Fatalf("schema version = %d, want 1", version)
	}

	var createdAt, updatedAt string
	if err := database.QueryRow(`
		SELECT created_at, updated_at
		FROM redirects
		WHERE slug = ?
	`, "known").Scan(&createdAt, &updatedAt); err != sql.ErrNoRows {
		t.Fatalf("query current columns error = %v, want %v", err, sql.ErrNoRows)
	}
}

func TestGetDBRejectsDuplicateSlugsCaseInsensitively(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(database.Close)

	if err := database.InsertRedirect("Known", "https://example.com/one"); err != nil {
		t.Fatalf("insert first redirect: %v", err)
	}
	if err := database.InsertRedirect("known", "https://example.com/two"); err == nil {
		t.Fatal("duplicate slug insert succeeded")
	}
}

func TestGetDBMigratesLegacySchemaAndPreservesRedirects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gottem.db")
	legacy := openSQLite(t, path)
	mustExec(t, legacy, `CREATE TABLE redirects (id INTEGER PRIMARY KEY, slug TEXT, url TEXT)`)
	mustExec(t, legacy, `INSERT INTO redirects (slug, url) VALUES (?, ?)`, "known", "https://example.com/destination")
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	database, err := db.GetDB(path)
	if err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	t.Cleanup(database.Close)

	target, err := database.QuerySlug("known")
	if err != nil {
		t.Fatalf("query migrated redirect: %v", err)
	}
	if target != "https://example.com/destination" {
		t.Fatalf("target = %q, want preserved destination", target)
	}

	var version int
	if err := database.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("query schema version: %v", err)
	}
	if version != 1 {
		t.Fatalf("schema version = %d, want 1", version)
	}

	var createdAt, updatedAt string
	if err := database.QueryRow(`
		SELECT created_at, updated_at
		FROM redirects
		WHERE slug = ?
	`, "known").Scan(&createdAt, &updatedAt); err != nil {
		t.Fatalf("query migrated timestamps: %v", err)
	}
	if createdAt == "" || updatedAt == "" {
		t.Fatalf("migrated timestamps = %q, %q; want non-empty", createdAt, updatedAt)
	}
}

func TestGetDBMigrationIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gottem.db")
	database, err := db.GetDB(path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.InsertRedirect("known", "https://example.com/destination"); err != nil {
		t.Fatalf("insert redirect: %v", err)
	}

	var createdAt string
	if err := database.QueryRow(`SELECT created_at FROM redirects WHERE slug = ?`, "known").Scan(&createdAt); err != nil {
		t.Fatalf("query creation timestamp: %v", err)
	}
	database.Close()

	reopened, err := db.GetDB(path)
	if err != nil {
		t.Fatalf("reopen migrated database: %v", err)
	}
	t.Cleanup(reopened.Close)

	var reopenedCreatedAt string
	if err := reopened.QueryRow(`SELECT created_at FROM redirects WHERE slug = ?`, "known").Scan(&reopenedCreatedAt); err != nil {
		t.Fatalf("query creation timestamp after reopen: %v", err)
	}
	if reopenedCreatedAt != createdAt {
		t.Fatalf("creation timestamp after reopen = %q, want %q", reopenedCreatedAt, createdAt)
	}
}

func TestGetDBRollsBackMigrationWhenLegacySlugsConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gottem.db")
	legacy := openSQLite(t, path)
	mustExec(t, legacy, `CREATE TABLE redirects (id INTEGER PRIMARY KEY, slug TEXT, url TEXT)`)
	mustExec(t, legacy, `INSERT INTO redirects (slug, url) VALUES (?, ?), (?, ?)`,
		"known", "https://example.com/one", "KNOWN", "https://example.com/two")
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	if database, err := db.GetDB(path); err == nil {
		database.Close()
		t.Fatal("migration succeeded despite conflicting legacy slugs")
	}

	after := openSQLite(t, path)
	t.Cleanup(func() { _ = after.Close() })

	var count int
	if err := after.QueryRow(`SELECT COUNT(*) FROM redirects`).Scan(&count); err != nil {
		t.Fatalf("count legacy rows after rollback: %v", err)
	}
	if count != 2 {
		t.Fatalf("legacy row count = %d, want 2", count)
	}

	var version int
	if err := after.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("query schema version after rollback: %v", err)
	}
	if version != 0 {
		t.Fatalf("schema version after rollback = %d, want 0", version)
	}
}

func openSQLite(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	return database
}

func mustExec(t *testing.T, database *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := database.Exec(query, args...); err != nil {
		t.Fatalf("execute %q: %v", query, err)
	}
}
