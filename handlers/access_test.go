package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"corwinm/gottem.link/handlers"
)

func TestInternalAccessHandlerRequiresLoopbackAndSharedToken(t *testing.T) {
	store := &internalAccessStore{}
	handler := handlers.InternalAccessHandler(store, "shared-management-token")
	body := `{"redirect_id":42,"accessed_at":"2026-01-02T03:04:05.123456789Z"}`

	for _, test := range []struct {
		name       string
		remoteAddr string
		token      string
		wantStatus int
	}{
		{name: "authorized proxy request", remoteAddr: "127.0.0.1:54321", token: "shared-management-token", wantStatus: http.StatusNoContent},
		{name: "missing token", remoteAddr: "127.0.0.1:54321", wantStatus: http.StatusNotFound},
		{name: "wrong token", remoteAddr: "127.0.0.1:54321", token: "wrong", wantStatus: http.StatusNotFound},
		{name: "non-loopback direct request", remoteAddr: "192.0.2.10:54321", token: "shared-management-token", wantStatus: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/.internal/accesses", strings.NewReader(body))
			request.RemoteAddr = test.remoteAddr
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
		})
	}

	if store.calls != 1 || store.id != 42 || store.at.Format(time.RFC3339Nano) != "2026-01-02T03:04:05.123456789Z" {
		t.Fatalf("store calls/value = %d/%d/%s", store.calls, store.id, store.at.Format(time.RFC3339Nano))
	}
}

func TestInternalAccessHandlerIsDisabledWithoutSharedToken(t *testing.T) {
	handler := handlers.InternalAccessHandler(&internalAccessStore{}, "")
	request := httptest.NewRequest(http.MethodPost, "/.internal/accesses", strings.NewReader(`{"redirect_id":1,"accessed_at":"2026-01-02T03:04:05Z"}`))
	request.RemoteAddr = "127.0.0.1:1234"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
}

func TestInternalMissHandlerValidatesAndStoresCanonicalAggregate(t *testing.T) {
	store := &internalMissStore{}
	handler := handlers.InternalMissHandler(store, "shared-management-token")

	for _, test := range []struct {
		name       string
		remoteAddr string
		token      string
		body       string
		wantStatus int
	}{
		{name: "authorized", remoteAddr: "127.0.0.1:54321", token: "shared-management-token", body: `{"slug":"shared-typo","missed_at":"2026-01-02T03:04:05.123456789Z"}`, wantStatus: http.StatusNoContent},
		{name: "non-loopback", remoteAddr: "192.0.2.10:54321", token: "shared-management-token", body: `{"slug":"shared-typo","missed_at":"2026-01-02T03:04:05Z"}`, wantStatus: http.StatusNotFound},
		{name: "invalid slug", remoteAddr: "127.0.0.1:54321", token: "shared-management-token", body: `{"slug":"not_valid","missed_at":"2026-01-02T03:04:05Z"}`, wantStatus: http.StatusBadRequest},
		{name: "invalid time", remoteAddr: "127.0.0.1:54321", token: "shared-management-token", body: `{"slug":"shared-typo","missed_at":"yesterday"}`, wantStatus: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/.internal/misses", strings.NewReader(test.body))
			request.RemoteAddr = test.remoteAddr
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
		})
	}
	if store.calls != 1 || store.slug != "shared-typo" || store.at.Format(time.RFC3339Nano) != "2026-01-02T03:04:05.123456789Z" {
		t.Fatalf("store calls/value = %d/%q/%s", store.calls, store.slug, store.at.Format(time.RFC3339Nano))
	}
}

type internalMissStore struct {
	calls int
	slug  string
	at    time.Time
}

func (store *internalMissStore) RecordSlugMiss(_ context.Context, slug string, at time.Time) error {
	store.calls++
	store.slug = slug
	store.at = at
	return nil
}

type internalAccessStore struct {
	calls int
	id    int64
	at    time.Time
}

func (store *internalAccessStore) RecordRedirectAccess(_ context.Context, id int64, at time.Time) error {
	store.calls++
	store.id = id
	store.at = at
	return nil
}
