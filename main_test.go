package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestParseConfigUsesNamedFlags(t *testing.T) {
	t.Setenv("GOTTEM_MANAGEMENT_TOKEN", "test-management-token")
	t.Setenv("GOTTEM_BACKUP_TOKEN", "test-backup-token")
	t.Setenv("GOTTEM_SESSION_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("GOTTEM_ADMIN_ORIGIN", "https://admin.example.com")
	config, err := parseConfig([]string{"-addr", ":9090", "-dsn", "/tmp/custom.db", "-migrate-only"})
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
	go func() { done <- serve(ctx, server, listener) }()

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
