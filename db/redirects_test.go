package db_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
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

func TestImportRedirectsAtomicallyPreservesDisabledState(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(database.Close)

	redirects := []db.ImportRedirect{
		{Slug: "active", URL: "https://example.com/active"},
		{Slug: "disabled", URL: "https://example.com/disabled", Disabled: true},
	}
	if err := database.ImportRedirects(redirects); err != nil {
		t.Fatalf("import redirects: %v", err)
	}
	if destination, err := database.QuerySlug("active"); err != nil || destination != "https://example.com/active" {
		t.Fatalf("active redirect = %q, %v", destination, err)
	}
	if _, err := database.QuerySlug("disabled"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("disabled redirect resolved with error %v", err)
	}
}

func TestImportRedirectsReportsSortedConflictsWithoutWrites(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(database.Close)
	if _, err := database.CreateRedirect("zeta", "https://example.com/existing"); err != nil {
		t.Fatalf("create existing redirect: %v", err)
	}
	if _, err := database.CreateRedirect("Alpha", "https://example.com/existing"); err != nil {
		t.Fatalf("create existing redirect: %v", err)
	}

	err = database.ImportRedirects([]db.ImportRedirect{
		{Slug: "ZETA", URL: "https://example.com/zeta"},
		{Slug: "new", URL: "https://example.com/new"},
		{Slug: "alpha", URL: "https://example.com/alpha"},
	})
	var conflict *db.SlugConflictsError
	if !errors.As(err, &conflict) {
		t.Fatalf("import error = %v, want slug conflicts", err)
	}
	if !reflect.DeepEqual(conflict.Slugs, []string{"alpha", "zeta"}) {
		t.Fatalf("conflicts = %#v, want alpha/zeta", conflict.Slugs)
	}
	if _, err := database.GetRedirect("new"); !errors.Is(err, db.ErrRedirectNotFound) {
		t.Fatalf("new redirect after rejected import error = %v", err)
	}
}
