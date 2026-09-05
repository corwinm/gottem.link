package db_test

import (
	"context"
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
