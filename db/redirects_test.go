package db_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"corwinm/gottem.link/db"
)

func TestRedirectRepositoryLifecycle(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(database.Close)

	created, err := database.CreateRedirect("Known", "https://example.com/one")
	if err != nil {
		t.Fatalf("create redirect: %v", err)
	}
	if created.ID == 0 || created.Slug != "Known" || created.URL != "https://example.com/one" || created.CreatedAt == "" || created.UpdatedAt == "" || created.DisabledAt != nil {
		t.Fatalf("created redirect = %#v", created)
	}

	listed, err := database.ListRedirects()
	if err != nil {
		t.Fatalf("list redirects: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("listed redirects = %#v, want created redirect", listed)
	}

	inspected, err := database.GetRedirect("known")
	if err != nil {
		t.Fatalf("inspect redirect case-insensitively: %v", err)
	}
	if inspected.ID != created.ID {
		t.Fatalf("inspected id = %d, want %d", inspected.ID, created.ID)
	}

	updated, err := database.UpdateRedirect("KNOWN", "https://example.com/two")
	if err != nil {
		t.Fatalf("update redirect: %v", err)
	}
	if updated.URL != "https://example.com/two" {
		t.Fatalf("updated URL = %q", updated.URL)
	}

	disabled, err := database.DisableRedirect("known")
	if err != nil {
		t.Fatalf("disable redirect: %v", err)
	}
	if disabled.DisabledAt == nil || *disabled.DisabledAt == "" {
		t.Fatalf("disabled_at = %#v, want timestamp", disabled.DisabledAt)
	}
	if _, err := database.QuerySlug("KNOWN"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("public query after disable error = %v, want %v", err, sql.ErrNoRows)
	}

	if err := database.DeleteRedirect("KNOWN"); err != nil {
		t.Fatalf("delete redirect: %v", err)
	}
	if _, err := database.GetRedirect("known"); !errors.Is(err, db.ErrRedirectNotFound) {
		t.Fatalf("inspect deleted redirect error = %v, want %v", err, db.ErrRedirectNotFound)
	}
}

func TestCreateRedirectReturnsSlugConflict(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(database.Close)

	if _, err := database.CreateRedirect("Known", "https://example.com/one"); err != nil {
		t.Fatalf("create first redirect: %v", err)
	}
	if _, err := database.CreateRedirect("known", "https://example.com/two"); !errors.Is(err, db.ErrSlugConflict) {
		t.Fatalf("duplicate create error = %v, want %v", err, db.ErrSlugConflict)
	}
}

func TestRedirectMutationsReturnNotFound(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(database.Close)

	if _, err := database.GetRedirect("missing"); !errors.Is(err, db.ErrRedirectNotFound) {
		t.Errorf("GetRedirect error = %v, want %v", err, db.ErrRedirectNotFound)
	}
	if _, err := database.UpdateRedirect("missing", "https://example.com"); !errors.Is(err, db.ErrRedirectNotFound) {
		t.Errorf("UpdateRedirect error = %v, want %v", err, db.ErrRedirectNotFound)
	}
	if _, err := database.DisableRedirect("missing"); !errors.Is(err, db.ErrRedirectNotFound) {
		t.Errorf("DisableRedirect error = %v, want %v", err, db.ErrRedirectNotFound)
	}
	if err := database.DeleteRedirect("missing"); !errors.Is(err, db.ErrRedirectNotFound) {
		t.Errorf("DeleteRedirect error = %v, want %v", err, db.ErrRedirectNotFound)
	}
}
