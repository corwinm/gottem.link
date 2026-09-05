package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

const currentSchemaVersion = 4

const redirectsV1Schema = `(
	id INTEGER PRIMARY KEY,
	slug TEXT NOT NULL COLLATE NOCASE UNIQUE,
	url TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`

const redirectsV3Schema = `(
	id INTEGER PRIMARY KEY,
	slug TEXT NOT NULL COLLATE NOCASE UNIQUE,
	url TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	disabled_at TEXT,
	expires_at TEXT,
	destination_updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`

const redirectsV4Schema = `(
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	slug TEXT NOT NULL COLLATE NOCASE UNIQUE,
	url TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	disabled_at TEXT,
	expires_at TEXT,
	destination_updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	click_count INTEGER NOT NULL DEFAULT 0 CHECK (click_count >= 0),
	last_accessed_at TEXT
)`

type DbWrapper struct {
	db *sql.DB
}

// Open returns a database handle without connecting or performing schema writes.
// Connection and schema failures are reported by operations that use the handle.
func Open(dsn string) (*DbWrapper, error) {
	database, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return &DbWrapper{database}, nil
}

// OpenAccessStore returns an isolated, single-connection handle for best-effort
// aggregate writes. It never waits on a rollback-journal lock held by reads.
func OpenAccessStore(dsn string) (*DbWrapper, error) {
	database, err := sql.Open("sqlite3", sqliteDSNParameter(dsn, "_busy_timeout", "0"))
	if err != nil {
		return nil, fmt.Errorf("open access store: %w", err)
	}
	database.SetMaxOpenConns(1)
	return &DbWrapper{database}, nil
}

func sqliteDSNParameter(dsn, name, value string) string {
	path, rawQuery, _ := strings.Cut(dsn, "?")
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return dsn
	}
	query.Set(name, value)
	return path + "?" + query.Encode()
}

// Migrate upgrades a database to the current schema and closes it.
func Migrate(dsn string) error {
	database, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	if err := migrate(database); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	return nil
}

// GetDB opens a database and migrates it to the current schema.
// It is retained for callers that need an initialized database handle.
func GetDB(dsn string) (*DbWrapper, error) {
	if err := Migrate(dsn); err != nil {
		return nil, err
	}
	return Open(dsn)
}

func migrate(database *sql.DB) error {
	var version int
	if err := database.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > currentSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than supported version %d", version, currentSchemaVersion)
	}

	for version < currentSchemaVersion {
		var err error
		switch version {
		case 0:
			err = migrateToVersion1(database)
		case 1:
			err = migrateToVersion2(database)
		case 2:
			err = migrateToVersion3(database)
		case 3:
			err = migrateToVersion4(database)
		}
		if err != nil {
			return err
		}
		version++
	}
	return nil
}

func migrateToVersion1(database *sql.DB) error {
	tx, err := database.Begin()
	if err != nil {
		return fmt.Errorf("begin version 1 migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var redirectsExist bool
	if err := tx.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM sqlite_schema
			WHERE type = 'table' AND name = 'redirects'
		)
	`).Scan(&redirectsExist); err != nil {
		return fmt.Errorf("inspect legacy schema: %w", err)
	}

	if redirectsExist {
		if _, err := tx.Exec("CREATE TABLE redirects_migration " + redirectsV1Schema); err != nil {
			return fmt.Errorf("create migrated redirects table: %w", err)
		}
		if _, err := tx.Exec(`
			INSERT INTO redirects_migration (id, slug, url, created_at, updated_at)
			SELECT id, slug, url, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
			FROM redirects
		`); err != nil {
			return fmt.Errorf("copy legacy redirects: %w", err)
		}
		if _, err := tx.Exec("DROP TABLE redirects"); err != nil {
			return fmt.Errorf("drop legacy redirects table: %w", err)
		}
		if _, err := tx.Exec("ALTER TABLE redirects_migration RENAME TO redirects"); err != nil {
			return fmt.Errorf("activate migrated redirects table: %w", err)
		}
	} else if _, err := tx.Exec("CREATE TABLE redirects " + redirectsV1Schema); err != nil {
		return fmt.Errorf("create redirects table: %w", err)
	}

	if _, err := tx.Exec("PRAGMA user_version = 1"); err != nil {
		return fmt.Errorf("record schema version 1: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit version 1 migration: %w", err)
	}
	return nil
}

func migrateToVersion2(database *sql.DB) error {
	tx, err := database.Begin()
	if err != nil {
		return fmt.Errorf("begin version 2 migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec("ALTER TABLE redirects ADD COLUMN disabled_at TEXT"); err != nil {
		return fmt.Errorf("add redirects.disabled_at: %w", err)
	}
	if _, err := tx.Exec("PRAGMA user_version = 2"); err != nil {
		return fmt.Errorf("record schema version 2: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit version 2 migration: %w", err)
	}
	return nil
}

func migrateToVersion3(database *sql.DB) error {
	tx, err := database.Begin()
	if err != nil {
		return fmt.Errorf("begin version 3 migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec("CREATE TABLE redirects_migration " + redirectsV3Schema); err != nil {
		return fmt.Errorf("create version 3 redirects table: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO redirects_migration (id, slug, url, created_at, updated_at, disabled_at, destination_updated_at)
		SELECT id, slug, url, created_at, updated_at, disabled_at, updated_at
		FROM redirects
	`); err != nil {
		return fmt.Errorf("copy version 2 redirects: %w", err)
	}
	if _, err := tx.Exec("DROP TABLE redirects"); err != nil {
		return fmt.Errorf("drop version 2 redirects table: %w", err)
	}
	if _, err := tx.Exec("ALTER TABLE redirects_migration RENAME TO redirects"); err != nil {
		return fmt.Errorf("activate version 3 redirects table: %w", err)
	}
	if _, err := tx.Exec("PRAGMA user_version = 3"); err != nil {
		return fmt.Errorf("record schema version 3: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit version 3 migration: %w", err)
	}
	return nil
}

func migrateToVersion4(database *sql.DB) error {
	tx, err := database.Begin()
	if err != nil {
		return fmt.Errorf("begin version 4 migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec("CREATE TABLE redirects_migration " + redirectsV4Schema); err != nil {
		return fmt.Errorf("create version 4 redirects table: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO redirects_migration (id, slug, url, created_at, updated_at, disabled_at, expires_at, destination_updated_at)
		SELECT id, slug, url, created_at, updated_at, disabled_at, expires_at, destination_updated_at FROM redirects`); err != nil {
		return fmt.Errorf("copy version 3 redirects: %w", err)
	}
	if _, err := tx.Exec("DROP TABLE redirects"); err != nil {
		return fmt.Errorf("drop version 3 redirects table: %w", err)
	}
	if _, err := tx.Exec("ALTER TABLE redirects_migration RENAME TO redirects"); err != nil {
		return fmt.Errorf("activate version 4 redirects table: %w", err)
	}
	if _, err := tx.Exec("PRAGMA user_version = 4"); err != nil {
		return fmt.Errorf("record schema version 4: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit version 4 migration: %w", err)
	}
	return nil
}

func (dbWrapper *DbWrapper) Close() {
	dbWrapper.db.Close()
}

// Exec a SQL statement.
func (db *DbWrapper) Exec(query string, args ...interface{}) (sql.Result, error) {
	return db.db.Exec(query, args...)
}

// Query a SQL statement.
func (db *DbWrapper) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return db.db.Query(query, args...)
}

// QueryRow queries a single row.
func (db *DbWrapper) QueryRow(query string, args ...interface{}) *sql.Row {
	return db.db.QueryRow(query, args...)
}

func (db *DbWrapper) Ready(ctx context.Context) error {
	var version int
	if err := db.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version != currentSchemaVersion {
		return fmt.Errorf("database schema version %d, want %d", version, currentSchemaVersion)
	}
	rows, err := db.db.QueryContext(ctx, "SELECT disabled_at, expires_at, destination_updated_at, click_count, last_accessed_at FROM redirects LIMIT 0")
	if err != nil {
		return err
	}
	return rows.Close()
}

func (db *DbWrapper) ResolveSlug(slug string) (int64, string, error) {
	var id int64
	var url string
	err := db.QueryRow("SELECT id, url FROM redirects WHERE slug = ? AND disabled_at IS NULL AND (expires_at IS NULL OR julianday(expires_at) > julianday('now'))", slug).Scan(&id, &url)
	if err != nil {
		return 0, "", err
	}
	return id, url, nil
}

func (db *DbWrapper) QuerySlug(slug string) (string, error) {
	_, url, err := db.ResolveSlug(slug)
	return url, err
}
