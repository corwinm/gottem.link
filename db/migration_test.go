package db_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"corwinm/gottem.link/db"
	_ "github.com/mattn/go-sqlite3"
)

func TestMigrateCreatesCurrentSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gottem.db")
	if err := db.Migrate(path); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	database := openSQLite(t, path)
	t.Cleanup(func() { _ = database.Close() })

	var version int
	if err := database.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("query schema version: %v", err)
	}
	if version != 1 {
		t.Fatalf("schema version = %d, want 1", version)
	}

	columns := map[string]struct {
		notNull      int
		defaultValue sql.NullString
	}{}
	rows, err := database.Query("PRAGMA table_info(redirects)")
	if err != nil {
		t.Fatalf("query redirects columns: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var position, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&position, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan redirects column: %v", err)
		}
		columns[name] = struct {
			notNull      int
			defaultValue sql.NullString
		}{notNull: notNull, defaultValue: defaultValue}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate redirects columns: %v", err)
	}

	for _, name := range []string{"slug", "url", "created_at", "updated_at"} {
		column, ok := columns[name]
		if !ok {
			t.Errorf("missing redirects.%s", name)
			continue
		}
		if column.notNull != 1 {
			t.Errorf("redirects.%s notnull = %d, want 1", name, column.notNull)
		}
	}
	for _, name := range []string{"created_at", "updated_at"} {
		if !columns[name].defaultValue.Valid || !strings.Contains(columns[name].defaultValue.String, "CURRENT_TIMESTAMP") {
			t.Errorf("redirects.%s default = %q, want CURRENT_TIMESTAMP", name, columns[name].defaultValue.String)
		}
	}
}

func TestMigrateRejectsDuplicateSlugsCaseInsensitively(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gottem.db")
	if err := db.Migrate(path); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	database := openSQLite(t, path)
	t.Cleanup(func() { _ = database.Close() })

	mustExec(t, database, `INSERT INTO redirects (slug, url) VALUES (?, ?)`, "Known", "https://example.com/one")
	if _, err := database.Exec(`INSERT INTO redirects (slug, url) VALUES (?, ?)`, "known", "https://example.com/two"); err == nil {
		t.Fatal("case-insensitive duplicate slug insert succeeded")
	}
}

func TestMigratePreservesLegacyRedirects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gottem.db")
	legacy := openSQLite(t, path)
	mustExec(t, legacy, `CREATE TABLE redirects (id INTEGER PRIMARY KEY, slug TEXT, url TEXT)`)
	mustExec(t, legacy, `INSERT INTO redirects (id, slug, url) VALUES (?, ?, ?)`, 42, "known", "https://example.com/destination")
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	if err := db.Migrate(path); err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}
	database := openSQLite(t, path)
	t.Cleanup(func() { _ = database.Close() })

	var id int
	var destination, createdAt, updatedAt string
	if err := database.QueryRow(`SELECT id, url, created_at, updated_at FROM redirects WHERE slug = ?`, "known").Scan(&id, &destination, &createdAt, &updatedAt); err != nil {
		t.Fatalf("query migrated redirect: %v", err)
	}
	if id != 42 || destination != "https://example.com/destination" {
		t.Fatalf("migrated redirect = (%d, %q), want (42, preserved destination)", id, destination)
	}
	if createdAt == "" || updatedAt == "" {
		t.Fatalf("migrated timestamps = %q, %q; want non-empty", createdAt, updatedAt)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gottem.db")
	if err := db.Migrate(path); err != nil {
		t.Fatalf("first migration: %v", err)
	}
	database := openSQLite(t, path)
	mustExec(t, database, `INSERT INTO redirects (slug, url) VALUES (?, ?)`, "known", "https://example.com/destination")
	var createdAt string
	if err := database.QueryRow(`SELECT created_at FROM redirects WHERE slug = ?`, "known").Scan(&createdAt); err != nil {
		t.Fatalf("query creation timestamp: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	if err := db.Migrate(path); err != nil {
		t.Fatalf("second migration: %v", err)
	}
	reopened := openSQLite(t, path)
	t.Cleanup(func() { _ = reopened.Close() })
	var reopenedCreatedAt string
	if err := reopened.QueryRow(`SELECT created_at FROM redirects WHERE slug = ?`, "known").Scan(&reopenedCreatedAt); err != nil {
		t.Fatalf("query creation timestamp after rerun: %v", err)
	}
	if reopenedCreatedAt != createdAt {
		t.Fatalf("creation timestamp after rerun = %q, want %q", reopenedCreatedAt, createdAt)
	}
}

func TestMigrateRollsBackConflictingLegacyData(t *testing.T) {
	tests := map[string][]any{
		"duplicate slugs": {"known", "https://example.com/one", "KNOWN", "https://example.com/two"},
		"null slug":       {nil, "https://example.com/one", "known", "https://example.com/two"},
		"null url":        {"one", nil, "known", "https://example.com/two"},
	}
	for name, values := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "gottem.db")
			legacy := openSQLite(t, path)
			mustExec(t, legacy, `CREATE TABLE redirects (id INTEGER PRIMARY KEY, slug TEXT, url TEXT)`)
			mustExec(t, legacy, `INSERT INTO redirects (slug, url) VALUES (?, ?), (?, ?)`, values...)
			if err := legacy.Close(); err != nil {
				t.Fatalf("close legacy database: %v", err)
			}

			if err := db.Migrate(path); err == nil {
				t.Fatal("migration succeeded despite incompatible legacy data")
			}

			after := openSQLite(t, path)
			t.Cleanup(func() { _ = after.Close() })
			var count, version int
			if err := after.QueryRow(`SELECT COUNT(*) FROM redirects`).Scan(&count); err != nil {
				t.Fatalf("count legacy rows after rollback: %v", err)
			}
			if err := after.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
				t.Fatalf("query version after rollback: %v", err)
			}
			if count != 2 || version != 0 {
				t.Fatalf("after rollback count/version = %d/%d, want 2/0", count, version)
			}
		})
	}
}

func TestMigrateRejectsNewerSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gottem.db")
	database := openSQLite(t, path)
	mustExec(t, database, `PRAGMA user_version = 2`)
	if err := database.Close(); err != nil {
		t.Fatalf("close newer database: %v", err)
	}

	if err := db.Migrate(path); err == nil {
		t.Fatal("migration accepted a newer schema version")
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
