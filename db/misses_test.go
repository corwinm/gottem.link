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
