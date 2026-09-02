package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestParseConfigUsesNamedFlags(t *testing.T) {
	config, err := parseConfig([]string{"-addr", ":9090", "-dsn", "/tmp/custom.db"})
	if err != nil {
		t.Fatalf("parseConfig returned an error: %v", err)
	}

	if config.addr != ":9090" {
		t.Errorf("addr = %q, want %q", config.addr, ":9090")
	}
	if config.dsn != "/tmp/custom.db" {
		t.Errorf("dsn = %q, want %q", config.dsn, "/tmp/custom.db")
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
