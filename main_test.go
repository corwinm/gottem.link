package main

import (
	"context"
	"corwinm/gottem.link/db"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"corwinm/gottem.link/routes"
)

func TestParseConfigUsesNamedFlags(t *testing.T) {
	t.Setenv("GOTTEM_MANAGEMENT_TOKEN", "test-management-token")
	t.Setenv("GOTTEM_BACKUP_TOKEN", "test-backup-token")
	t.Setenv("GOTTEM_SESSION_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("GOTTEM_ADMIN_ORIGIN", "https://admin.example.com")
	config, err := parseConfig([]string{"-addr", ":9090", "-dsn", "/tmp/custom.db", "-stats-proxy-url", "http://127.0.0.1:8080", "-migrate-only"})
	if err != nil {
		t.Fatalf("parseConfig returned an error: %v", err)
	}

	if config.addr != ":9090" {
		t.Errorf("addr = %q, want %q", config.addr, ":9090")
	}
	if config.dsn != "/tmp/custom.db" {
		t.Errorf("dsn = %q, want %q", config.dsn, "/tmp/custom.db")
	}
	if !config.migrateOnly {
		t.Error("migrateOnly = false, want true")
	}
	if config.statsProxyURL != "http://127.0.0.1:8080" {
		t.Errorf("statsProxyURL = %q", config.statsProxyURL)
	}
	if config.managementToken != "test-management-token" {
		t.Errorf("managementToken was not read from GOTTEM_MANAGEMENT_TOKEN")
	}
	if config.backupToken != "test-backup-token" {
		t.Errorf("backupToken was not read from GOTTEM_BACKUP_TOKEN")
	}
	if config.sessionSecret != "0123456789abcdef0123456789abcdef" || config.adminOrigin != "https://admin.example.com" || !config.secureCookies {
		t.Errorf("admin config = secret %q, origin %q, secure %v", config.sessionSecret, config.adminOrigin, config.secureCookies)
	}
}

func TestParseConfigDisablesStatsUnlessProxyIsConfigured(t *testing.T) {
	t.Setenv("GOTTEM_MANAGEMENT_TOKEN", "test-management-token")
	t.Setenv("GOTTEM_BACKUP_TOKEN", "")
	t.Setenv("GOTTEM_SESSION_SECRET", "")
	t.Setenv("GOTTEM_ADMIN_ORIGIN", "")
	config, err := parseConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if config.statsProxyURL != "" {
		t.Fatalf("statsProxyURL = %q, want disabled", config.statsProxyURL)
	}
}

func TestParseConfigRejectsMatchingManagementAndBackupTokens(t *testing.T) {
	t.Setenv("GOTTEM_SESSION_SECRET", "")
	t.Setenv("GOTTEM_ADMIN_ORIGIN", "")
	t.Setenv("GOTTEM_MANAGEMENT_TOKEN", "same-token")
	t.Setenv("GOTTEM_BACKUP_TOKEN", "same-token")
	if _, err := parseConfig(nil); err == nil {
		t.Fatal("parseConfig accepted matching management and backup tokens")
	}
}

func TestParseConfigAdminSetup(t *testing.T) {
	t.Setenv("GOTTEM_MANAGEMENT_TOKEN", "test-management-token")
	t.Setenv("GOTTEM_BACKUP_TOKEN", "")
	tests := []struct {
		name       string
		secret     string
		origin     string
		wantError  bool
		wantSecure bool
	}{
		{name: "disabled"},
		{name: "https", secret: "0123456789abcdef0123456789abcdef", origin: "https://admin.example.com", wantSecure: true},
		{name: "loopback HTTP", secret: "0123456789abcdef0123456789abcdef", origin: "http://127.0.0.1:8080"},
		{name: "localhost HTTP", secret: "0123456789abcdef0123456789abcdef", origin: "http://localhost:8080"},
		{name: "missing secret", origin: "https://admin.example.com", wantError: true},
		{name: "missing origin", secret: "0123456789abcdef0123456789abcdef", wantError: true},
		{name: "short secret", secret: "too-short", origin: "https://admin.example.com", wantError: true},
		{name: "HTTP public host", secret: "0123456789abcdef0123456789abcdef", origin: "http://admin.example.com", wantError: true},
		{name: "origin path", secret: "0123456789abcdef0123456789abcdef", origin: "https://admin.example.com/path", wantError: true},
		{name: "origin trailing slash", secret: "0123456789abcdef0123456789abcdef", origin: "https://admin.example.com/", wantError: true},
		{name: "origin credentials", secret: "0123456789abcdef0123456789abcdef", origin: "https://user@admin.example.com", wantError: true},
		{name: "HTTPS default port", secret: "0123456789abcdef0123456789abcdef", origin: "https://admin.example.com:443", wantError: true},
		{name: "HTTP default port", secret: "0123456789abcdef0123456789abcdef", origin: "http://localhost:80", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("GOTTEM_SESSION_SECRET", test.secret)
			t.Setenv("GOTTEM_ADMIN_ORIGIN", test.origin)
			config, err := parseConfig(nil)
			if (err != nil) != test.wantError {
				t.Fatalf("parseConfig error = %v, wantError %v", err, test.wantError)
			}
			if err == nil && config.secureCookies != test.wantSecure {
				t.Fatalf("secureCookies = %v, want %v", config.secureCookies, test.wantSecure)
			}
		})
	}
}

func TestServeWaitsForActiveRequestDuringShutdown(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, server, listener, nil) }()

	requestDone := make(chan error, 1)
	go func() {
		request, err := http.NewRequest(http.MethodGet, "http://"+listener.Addr().String(), nil)
		if err != nil {
			requestDone <- err
			return
		}
		response, err := http.DefaultClient.Do(request)
		if err == nil {
			response.Body.Close()
		}
		requestDone <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not reach handler")
	}
	cancel()

	select {
	case err := <-done:
		t.Fatalf("serve returned before active request completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	if err := <-requestDone; err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("serve returned an error: %v", err)
	}
}

func TestShutdownUsesOneDeadlineForServerAndAccessWriter(t *testing.T) {
	store := &shutdownBlockingStore{started: make(chan struct{})}
	writer := db.NewAccessWriter(store, 1, nil)
	writer.Track(1, time.Now())
	<-store.started
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := shutdown(ctx, &http.Server{}, writer)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("shutdown exceeded its deadline: %s", elapsed)
	}
}

func TestShutdownKeepsInternalReceiverAvailableWhileDraining(t *testing.T) {
	database, err := db.GetDB(filepath.Join(t.TempDir(), "gottem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	redirect, err := database.CreateRedirect("known", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpStore, err := db.NewHTTPAccessStore("http://"+listener.Addr().String(), "shared-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	gate := &gatedAccessStore{inner: httpStore, started: make(chan struct{}), release: make(chan struct{})}
	writer := db.NewAccessWriter(gate, 1, nil)
	server := &http.Server{Handler: routes.NewRouterWithAdminStats(database, "shared-token", "", routes.AdminConfig{}, writer, database)}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	writer.Track(redirect.ID, time.Now())
	<-gate.started
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- shutdown(ctx, server, writer) }()
	time.Sleep(25 * time.Millisecond)
	close(gate.release)
	if err := <-shutdownDone; err != nil {
		t.Fatal(err)
	}
	if err := <-serveDone; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Serve error = %v", err)
	}
	stored, err := database.GetRedirect("known")
	if err != nil || stored.ClickCount != 1 {
		t.Fatalf("stored = %#v, err = %v", stored, err)
	}
}

type gatedAccessStore struct {
	inner   db.AccessStore
	started chan struct{}
	release chan struct{}
}

func (store *gatedAccessStore) RecordRedirectAccess(ctx context.Context, id int64, at time.Time) error {
	close(store.started)
	<-store.release
	return store.inner.RecordRedirectAccess(ctx, id, at)
}

type shutdownBlockingStore struct{ started chan struct{} }

func (store *shutdownBlockingStore) RecordRedirectAccess(ctx context.Context, _ int64, _ time.Time) error {
	close(store.started)
	<-ctx.Done()
	return ctx.Err()
}
