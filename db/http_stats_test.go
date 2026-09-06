package db_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"corwinm/gottem.link/db"
)

func TestHTTPAccessStorePostsOnlyAggregateToLiteFSProxy(t *testing.T) {
	requestSeen := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/.internal/accesses" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if authorization := r.Header.Get("Authorization"); authorization != "Bearer shared-management-token" {
			t.Errorf("Authorization = %q", authorization)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		const want = `{"redirect_id":42,"accessed_at":"2026-01-02T01:04:05.123456789Z"}` + "\n"
		if string(body) != want {
			t.Errorf("body = %q, want %q", body, want)
		}
		w.WriteHeader(http.StatusNoContent)
		requestSeen <- struct{}{}
	}))
	t.Cleanup(server.Close)

	store, err := db.NewHTTPAccessStore(server.URL, "shared-management-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.FixedZone("offset", 2*60*60))
	if err := store.RecordRedirectAccess(context.Background(), 42, at); err != nil {
		t.Fatal(err)
	}
	<-requestSeen
}

func TestHTTPAccessStoreRejectsMissingSharedToken(t *testing.T) {
	if _, err := db.NewHTTPAccessStore("http://127.0.0.1:8080", "", http.DefaultClient); err == nil {
		t.Fatal("NewHTTPAccessStore accepted an empty token")
	}
}

func TestHTTPAccessStoreForwardsSlugMiss(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/.internal/misses" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer shared-token" {
			t.Errorf("Authorization = %q", got)
		}
		var body struct {
			Slug     string `json:"slug"`
			MissedAt string `json:"missed_at"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Slug != "shared-typo" || body.MissedAt != "2026-04-05T06:07:08.000000009Z" {
			t.Errorf("body = %#v", body)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	store, err := db.NewHTTPAccessStore(server.URL, "shared-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 4, 5, 6, 7, 8, 9, time.UTC)
	if err := store.RecordSlugMiss(context.Background(), "shared-typo", at); err != nil {
		t.Fatal(err)
	}
}
