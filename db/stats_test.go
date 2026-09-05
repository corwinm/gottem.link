package db_test

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"corwinm/gottem.link/db"
	"corwinm/gottem.link/routes"
)

func TestRecordRedirectAccessIsAtomicAndKeepsLatestUTC(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	created, err := database.CreateRedirect("known", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	older := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("offset", 2*60*60))
	newer := older.Add(time.Hour)

	var wg sync.WaitGroup
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := database.RecordRedirectAccess(context.Background(), created.ID, older); err != nil {
				t.Errorf("record access: %v", err)
			}
		}()
	}
	wg.Wait()
	if err := database.RecordRedirectAccess(context.Background(), created.ID, newer); err != nil {
		t.Fatal(err)
	}
	if err := database.RecordRedirectAccess(context.Background(), created.ID, older); err != nil {
		t.Fatal(err)
	}

	stored, err := database.GetRedirect("known")
	if err != nil {
		t.Fatal(err)
	}
	if stored.ClickCount != 42 || stored.LastAccessedAt == nil || *stored.LastAccessedAt != newer.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("stats = %d/%v", stored.ClickCount, stored.LastAccessedAt)
	}
}

func TestRecordRedirectAccessSaturatesAtMaxInt64(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	redirect, err := database.CreateRedirect("known", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("UPDATE redirects SET click_count = ? WHERE id = ?", int64(math.MaxInt64), redirect.ID); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 2, 3, 4, 5, 6, 7, time.UTC)
	if err := database.RecordRedirectAccess(context.Background(), redirect.ID, at); err != nil {
		t.Fatal(err)
	}
	stored, err := database.GetRedirect("known")
	if err != nil {
		t.Fatal(err)
	}
	if stored.ClickCount != math.MaxInt64 || stored.LastAccessedAt == nil || *stored.LastAccessedAt != at.Format(time.RFC3339Nano) {
		t.Fatalf("stats = %d/%v", stored.ClickCount, stored.LastAccessedAt)
	}
}

func TestRedirectIDsAreNotReusedForQueuedAccessSafety(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	first, err := database.CreateRedirect("first", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.DeleteRedirect(first.Slug); err != nil {
		t.Fatal(err)
	}
	second, err := database.CreateRedirect("second", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID <= first.ID {
		t.Fatalf("redirect ID was reused: first=%d second=%d", first.ID, second.ID)
	}
}

func TestAccessStoreDoesNotWaitBehindRollbackJournalReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gottem.db")
	database, err := db.GetDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	redirect, err := database.CreateRedirect("known", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := database.Query("SELECT id FROM redirects")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("missing redirect")
	}

	store, err := db.OpenAccessStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	started := time.Now()
	err = store.RecordRedirectAccess(context.Background(), redirect.ID, time.Now())
	if err == nil {
		t.Fatal("access write unexpectedly succeeded while a rollback-journal reader held the lock")
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("contended access write waited %s", elapsed)
	}
	if _, _, err := database.ResolveSlug("known"); err != nil {
		t.Fatalf("redirect read failed during contention: %v", err)
	}
}

func TestAccessStoreOverridesTimeoutAlias(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gottem.db")
	database, err := db.GetDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	redirect, err := database.CreateRedirect("known", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := database.Query("SELECT id FROM redirects")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("missing redirect")
	}

	store, err := db.OpenAccessStore(path + "?_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	started := time.Now()
	err = store.RecordRedirectAccess(context.Background(), redirect.ID, time.Now())
	if err == nil {
		t.Fatal("access write unexpectedly succeeded while a rollback-journal reader held the lock")
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("_timeout alias kept access write blocked for %s", elapsed)
	}
}

func TestRealSQLiteRedirectReadsRemainAvailableDuringAccessWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gottem.db")
	database, err := db.GetDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	redirect, err := database.CreateRedirect("known", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	store, err := db.OpenAccessStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	writer := db.NewAccessWriter(store, 256, nil)
	router := routes.NewRouterWithStats(database, "", writer)

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			writer.Track(redirect.ID, time.Now())
		}()
		go func() {
			defer wg.Done()
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/known", nil))
			if response.Code != http.StatusFound {
				t.Errorf("redirect status = %d", response.Code)
			}
		}()
	}
	wg.Wait()
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAccessWriterIsNonBlockingWhenStorageIsSlowAndQueueIsFull(t *testing.T) {
	store := &blockingAccessStore{started: make(chan struct{}), release: make(chan struct{})}
	writer := db.NewAccessWriter(store, 1, nil)
	t.Cleanup(func() { _ = writer.Close(context.Background()) })
	if !writer.Track(1, time.Now()) {
		t.Fatal("first access was dropped")
	}
	<-store.started
	if !writer.Track(2, time.Now()) {
		t.Fatal("queued access was dropped")
	}
	started := time.Now()
	if writer.Track(3, time.Now()) {
		t.Fatal("saturated writer accepted an access")
	}
	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
		t.Fatalf("saturated Track blocked for %s", elapsed)
	}
	close(store.release)
}

func TestAccessWriterContinuesAfterStorageFailure(t *testing.T) {
	store := &recordingAccessStore{failFirst: true}
	errorsSeen := make(chan error, 1)
	writer := db.NewAccessWriter(store, 4, func(err error) { errorsSeen <- err })
	if !writer.Track(1, time.Unix(1, 0)) || !writer.Track(2, time.Unix(2, 0)) {
		t.Fatal("writer unexpectedly dropped access")
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-errorsSeen; err == nil {
		t.Fatal("failure callback received nil")
	}
	if got := store.successes.Load(); got != 1 {
		t.Fatalf("successful writes = %d, want 1", got)
	}
}

func TestAccessWriterCloseDrainsAcceptedWorkAndIsConcurrentSafe(t *testing.T) {
	store := &recordingAccessStore{}
	writer := db.NewAccessWriter(store, 256, nil)
	var accepted atomic.Int64
	var wg sync.WaitGroup
	for index := range 100 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if writer.Track(int64(id+1), time.Now()) {
				accepted.Add(1)
			}
		}(index)
	}
	wg.Wait()
	var closeWG sync.WaitGroup
	for range 4 {
		closeWG.Add(1)
		go func() {
			defer closeWG.Done()
			if err := writer.Close(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	closeWG.Wait()
	if writer.Track(999, time.Now()) {
		t.Fatal("closed writer accepted access")
	}
	if got := store.successes.Load(); got != accepted.Load() {
		t.Fatalf("writes = %d, accepted = %d", got, accepted.Load())
	}
}

func TestAccessWriterCloseStopsAtDeadlineAndDropsQueuedWork(t *testing.T) {
	store := &contextBlockingAccessStore{started: make(chan struct{})}
	writer := db.NewAccessWriter(store, 4, nil)
	if !writer.Track(1, time.Now()) || !writer.Track(2, time.Now()) {
		t.Fatal("writer dropped work before shutdown")
	}
	<-store.started
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := writer.Close(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("Close exceeded deadline by too much: %s", elapsed)
	}
	if calls := store.calls.Load(); calls != 1 {
		t.Fatalf("storage calls = %d, want queued work dropped", calls)
	}
}

type contextBlockingAccessStore struct {
	started chan struct{}
	calls   atomic.Int64
}

func (store *contextBlockingAccessStore) RecordRedirectAccess(ctx context.Context, _ int64, _ time.Time) error {
	if store.calls.Add(1) == 1 {
		close(store.started)
	}
	<-ctx.Done()
	return ctx.Err()
}

type blockingAccessStore struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingAccessStore) RecordRedirectAccess(_ context.Context, _ int64, _ time.Time) error {
	s.once.Do(func() { close(s.started) })
	<-s.release
	return nil
}

type recordingAccessStore struct {
	failFirst bool
	calls     atomic.Int64
	successes atomic.Int64
}

func (s *recordingAccessStore) RecordRedirectAccess(_ context.Context, _ int64, _ time.Time) error {
	if s.failFirst && s.calls.Add(1) == 1 {
		return errors.New("forced storage failure")
	}
	s.successes.Add(1)
	return nil
}
