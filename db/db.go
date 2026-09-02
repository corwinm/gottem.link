package db

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

const currentSchemaVersion = 1

const redirectsSchema = `(
	id INTEGER PRIMARY KEY,
	slug TEXT NOT NULL COLLATE NOCASE UNIQUE,
	url TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
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

// GetDB opens a database and migrates it to the current schema.
func GetDB(dsn string) (*DbWrapper, error) {
	database, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := migrate(database); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}

	return &DbWrapper{database}, nil
}

func migrate(database *sql.DB) error {
	var version int
	if err := database.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > currentSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than supported version %d", version, currentSchemaVersion)
	}
	if version == currentSchemaVersion {
		return nil
	}

	tx, err := database.Begin()
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var redirectsExist bool
	if err := tx.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM sqlite_master
			WHERE type = 'table' AND name = 'redirects'
		)
	`).Scan(&redirectsExist); err != nil {
		return fmt.Errorf("inspect legacy schema: %w", err)
	}

	if redirectsExist {
		if _, err := tx.Exec("CREATE TABLE redirects_migration " + redirectsSchema); err != nil {
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
	} else {
		if _, err := tx.Exec("CREATE TABLE redirects " + redirectsSchema); err != nil {
			return fmt.Errorf("create redirects table: %w", err)
		}
	}

	if _, err := tx.Exec("PRAGMA user_version = 1"); err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
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

func (db *DbWrapper) QuerySlug(slug string) (string, error) {
	var url string
	err := db.QueryRow("SELECT url FROM redirects WHERE slug = ?", slug).Scan(&url)
	if err != nil {
		return "", err
	}
	return url, nil
}

func (db *DbWrapper) InsertRedirect(slug, url string) error {
	_, err := db.Exec("INSERT INTO redirects (slug, url) VALUES (?, ?)", slug, url)
	if err != nil {
		return err
	}
	return nil
}

func (db *DbWrapper) DeleteRedirect(slug string) error {
	_, err := db.Exec("DELETE FROM redirects WHERE slug = ?", slug)
	if err != nil {
		return err
	}
	return nil
}
