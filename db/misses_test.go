package db_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"corwinm/gottem.link/db"
)

func TestRecordSlugMissAggregatesCaseInsensitivelyAndKeepsTimestampRange(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	first := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("offset", 2*60*60))
	last := first.Add(2 * time.Hour)

	var wait sync.WaitGroup
	for range 20 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := database.RecordSlugMiss(context.Background(), "Shared-Typo", first); err != nil {
				t.Errorf("record miss: %v", err)
			}
		}()
	}
	wait.Wait()
	if err := database.RecordSlugMiss(context.Background(), "shared-typo", last); err != nil {
		t.Fatal(err)
	}
	if err := database.RecordSlugMiss(context.Background(), "SHARED-TYPO", first); err != nil {
		t.Fatal(err)
	}

	misses, err := database.ListSlugMisses()
	if err != nil {
		t.Fatal(err)
	}
	if len(misses) != 1 {
		t.Fatalf("misses = %#v, want one aggregate", misses)
	}
	miss := misses[0]
	if miss.Slug != "shared-typo" || miss.MissCount != 22 || miss.FirstMissedAt != first.UTC().Format(time.RFC3339Nano) || miss.LastMissedAt != last.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("miss = %#v", miss)
	}
}

func TestCreatingRedirectRemovesResolvedSlugMiss(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if err := database.RecordSlugMiss(context.Background(), "shared-typo", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateRedirect("shared-typo", "https://example.com/corrected"); err != nil {
		t.Fatal(err)
	}
	if err := database.RecordSlugMiss(context.Background(), "shared-typo", time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	misses, err := database.ListSlugMisses()
	if err != nil {
		t.Fatal(err)
	}
	if len(misses) != 0 {
		t.Fatalf("resolved misses = %#v, want none", misses)
	}
}

func TestImportingRedirectRemovesResolvedSlugMiss(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if err := database.RecordSlugMiss(context.Background(), "shared-typo", time.Now()); err != nil {
		t.Fatal(err)
	}

	if err := database.ImportRedirects([]db.ImportRedirect{{
		Slug: "SHARED-TYPO",
		URL:  "https://example.com/corrected",
	}}); err != nil {
		t.Fatal(err)
	}

	misses, err := database.ListSlugMisses()
	if err != nil {
		t.Fatal(err)
	}
	if len(misses) != 0 {
		t.Fatalf("resolved misses = %#v, want none", misses)
	}
}

func TestSlugMissRetentionIsBoundedToMostRecentFiveHundred(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for index := range 501 {
		slug := "miss-" + time.Unix(int64(index), 0).UTC().Format("150405")
		if err := database.RecordSlugMiss(context.Background(), slug, base.Add(time.Duration(index)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM slug_misses").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 500 {
		t.Fatalf("stored miss rows = %d, want 500", count)
	}
	var oldestExists bool
	if err := database.QueryRow("SELECT EXISTS(SELECT 1 FROM slug_misses WHERE slug = 'miss-000000')").Scan(&oldestExists); err != nil {
		t.Fatal(err)
	}
	if oldestExists {
		t.Error("oldest miss was not pruned")
	}
}

func TestRecordSlugMissRollsBackWhenRetentionPruningFails(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if _, err := database.Exec(`
		WITH RECURSIVE numbers(value) AS (
			VALUES(0)
			UNION ALL
			SELECT value + 1 FROM numbers WHERE value < 499
		)
		INSERT INTO slug_misses (slug, miss_count, first_missed_at, last_missed_at)
		SELECT printf('miss-%03d', value), 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z'
		FROM numbers
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		CREATE TRIGGER reject_slug_miss_pruning
		BEFORE DELETE ON slug_misses
		BEGIN
			SELECT RAISE(ABORT, 'forced prune failure');
		END
	`); err != nil {
		t.Fatal(err)
	}

	err = database.RecordSlugMiss(context.Background(), "newest", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("record miss succeeded despite prune failure")
	}
	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM slug_misses").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 500 {
		t.Fatalf("stored miss rows after prune failure = %d, want 500", count)
	}
	var newestExists bool
	if err := database.QueryRow("SELECT EXISTS(SELECT 1 FROM slug_misses WHERE slug = 'newest')").Scan(&newestExists); err != nil {
		t.Fatal(err)
	}
	if newestExists {
		t.Error("new miss survived failed retention pruning")
	}
}
