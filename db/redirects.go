package db

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/mattn/go-sqlite3"
)

var (
	ErrRedirectNotFound = errors.New("redirect not found")
	ErrSlugConflict     = errors.New("slug already exists")
)

type Redirect struct {
	ID         int64   `json:"id"`
	Slug       string  `json:"slug"`
	URL        string  `json:"url"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
	DisabledAt *string `json:"disabled_at"`
}

type ImportRedirect struct {
	Slug     string
	URL      string
	Disabled bool
}

type SlugConflictsError struct {
	Slugs []string
}

func (err *SlugConflictsError) Error() string {
	return "slug conflicts: " + strings.Join(err.Slugs, ", ")
}

const redirectColumns = "id, slug, url, created_at, updated_at, disabled_at"

func (db *DbWrapper) CreateRedirect(slug, url string) (Redirect, error) {
	redirect, err := scanRedirect(db.QueryRow(`
		INSERT INTO redirects (slug, url)
		VALUES (?, ?)
		RETURNING `+redirectColumns,
		slug, url,
	))
	if isUniqueConstraint(err) {
		return Redirect{}, ErrSlugConflict
	}
	if err != nil {
		return Redirect{}, fmt.Errorf("create redirect: %w", err)
	}
	return redirect, nil
}

func (db *DbWrapper) ListRedirects() ([]Redirect, error) {
	rows, err := db.Query("SELECT " + redirectColumns + " FROM redirects ORDER BY slug COLLATE NOCASE")
	if err != nil {
		return nil, fmt.Errorf("list redirects: %w", err)
	}
	defer rows.Close()

	redirects := make([]Redirect, 0)
	for rows.Next() {
		redirect, err := scanRedirect(rows)
		if err != nil {
			return nil, fmt.Errorf("scan redirect: %w", err)
		}
		redirects = append(redirects, redirect)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list redirects: %w", err)
	}
	return redirects, nil
}

func (db *DbWrapper) ImportRedirects(redirects []ImportRedirect) error {
	tx, err := db.db.Begin()
	if err != nil {
		return fmt.Errorf("begin redirect import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	conflicts := make([]string, 0)
	for _, redirect := range redirects {
		var exists bool
		if err := tx.QueryRow("SELECT EXISTS (SELECT 1 FROM redirects WHERE slug = ?)", redirect.Slug).Scan(&exists); err != nil {
			return fmt.Errorf("check redirect import conflicts: %w", err)
		}
		if exists {
			conflicts = append(conflicts, sqliteNOCASEName(redirect.Slug))
		}
	}
	if len(conflicts) > 0 {
		sort.Strings(conflicts)
		return &SlugConflictsError{Slugs: conflicts}
	}

	for _, redirect := range redirects {
		_, err := tx.Exec(`
			INSERT INTO redirects (slug, url, disabled_at)
			VALUES (?, ?, CASE WHEN ? THEN CURRENT_TIMESTAMP ELSE NULL END)
		`, redirect.Slug, redirect.URL, redirect.Disabled)
		if isUniqueConstraint(err) {
			return &SlugConflictsError{Slugs: []string{sqliteNOCASEName(redirect.Slug)}}
		}
		if err != nil {
			return fmt.Errorf("import redirect: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit redirect import: %w", err)
	}
	return nil
}

func (db *DbWrapper) RedirectImportConflicts(redirects []ImportRedirect) ([]string, error) {
	conflicts := make([]string, 0)
	for _, redirect := range redirects {
		var exists bool
		if err := db.QueryRow("SELECT EXISTS (SELECT 1 FROM redirects WHERE slug = ?)", redirect.Slug).Scan(&exists); err != nil {
			return nil, fmt.Errorf("check redirect import conflicts: %w", err)
		}
		if exists {
			conflicts = append(conflicts, sqliteNOCASEName(redirect.Slug))
		}
	}
	sort.Strings(conflicts)
	return conflicts, nil
}

func (db *DbWrapper) GetRedirect(slug string) (Redirect, error) {
	redirect, err := scanRedirect(db.QueryRow(
		"SELECT "+redirectColumns+" FROM redirects WHERE slug = ?",
		slug,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return Redirect{}, ErrRedirectNotFound
	}
	if err != nil {
		return Redirect{}, fmt.Errorf("get redirect: %w", err)
	}
	return redirect, nil
}

func (db *DbWrapper) UpdateRedirect(slug, url string) (Redirect, error) {
	redirect, err := scanRedirect(db.QueryRow(`
		UPDATE redirects
		SET url = ?, updated_at = CURRENT_TIMESTAMP
		WHERE slug = ?
		RETURNING `+redirectColumns,
		url, slug,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return Redirect{}, ErrRedirectNotFound
	}
	if err != nil {
		return Redirect{}, fmt.Errorf("update redirect: %w", err)
	}
	return redirect, nil
}

func (db *DbWrapper) DisableRedirect(slug string) (Redirect, error) {
	redirect, err := scanRedirect(db.QueryRow(`
		UPDATE redirects
		SET disabled_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE slug = ?
		RETURNING `+redirectColumns,
		slug,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return Redirect{}, ErrRedirectNotFound
	}
	if err != nil {
		return Redirect{}, fmt.Errorf("disable redirect: %w", err)
	}
	return redirect, nil
}

func (db *DbWrapper) InsertRedirect(slug, url string) error {
	_, err := db.CreateRedirect(slug, url)
	return err
}

func (db *DbWrapper) DeleteRedirect(slug string) error {
	result, err := db.Exec("DELETE FROM redirects WHERE slug = ?", slug)
	if err != nil {
		return fmt.Errorf("delete redirect: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete redirect result: %w", err)
	}
	if rowsAffected == 0 {
		return ErrRedirectNotFound
	}
	return nil
}

type redirectScanner interface {
	Scan(dest ...any) error
}

func scanRedirect(scanner redirectScanner) (Redirect, error) {
	var redirect Redirect
	var disabledAt sql.NullString
	if err := scanner.Scan(
		&redirect.ID,
		&redirect.Slug,
		&redirect.URL,
		&redirect.CreatedAt,
		&redirect.UpdatedAt,
		&disabledAt,
	); err != nil {
		return Redirect{}, err
	}
	if disabledAt.Valid {
		redirect.DisabledAt = &disabledAt.String
	}
	return redirect, nil
}

func isUniqueConstraint(err error) bool {
	var sqliteErr sqlite3.Error
	return errors.As(err, &sqliteErr) && sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique
}

func sqliteNOCASEName(value string) string {
	folded := []byte(value)
	for index, character := range folded {
		if character >= 'A' && character <= 'Z' {
			folded[index] = character + ('a' - 'A')
		}
	}
	return string(folded)
}
