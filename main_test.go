package main

import "testing"

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
