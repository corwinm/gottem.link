package db

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

type DbWrapper struct {
	db *sql.DB
}

type TableMeta struct {
	Table string
}

// GetDB returns a new instance of sql.DB initialized with a database connection
func GetDB(dsn string) (*DbWrapper, error) {
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	err = ensureTableExists(db, "redirects", TableMeta{Table: "(id INTEGER PRIMARY KEY, slug TEXT, url TEXT)"})
	if err != nil {
		return nil, err
	}

	return &DbWrapper{db}, nil
}

// EnsureTableExists sets up a table if it doesn't exist
func ensureTableExists(dbWrapper *sql.DB, table string, tableMeta TableMeta) error {
	// If table does not exist, create it
	sqlStatement := `CREATE TABLE IF NOT EXISTS ` + table + ` ` + tableMeta.Table
	_, err := dbWrapper.Exec(sqlStatement)
	if err != nil {
		return err
	}
	return nil
}

func (dbWrapper *DbWrapper) Close() {
	dbWrapper.db.Close()
}

// Exec a SQL statement
func (db *DbWrapper) Exec(query string, args ...interface{}) (sql.Result, error) {
	return db.db.Exec(query, args...)
}

// Query a SQL statement
func (db *DbWrapper) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return db.db.Query(query, args...)
}

// QueryRow a SQL statement
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
