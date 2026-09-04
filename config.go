package main

import (
	"errors"
	"flag"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
)

type config struct {
	addr            string
	dsn             string
	migrateOnly     bool
	managementToken string
	backupToken     string
	sessionSecret   string
	adminOrigin     string
	secureCookies   bool
}

func parseConfig(args []string) (config, error) {
	flags := flag.NewFlagSet("gottem.link", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	var config config
	config.managementToken = os.Getenv("GOTTEM_MANAGEMENT_TOKEN")
	config.backupToken = os.Getenv("GOTTEM_BACKUP_TOKEN")
	config.sessionSecret = os.Getenv("GOTTEM_SESSION_SECRET")
	config.adminOrigin = os.Getenv("GOTTEM_ADMIN_ORIGIN")
	flags.StringVar(&config.addr, "addr", ":8080", "http service address")
	flags.StringVar(&config.dsn, "dsn", "gottem.db", "database file")
	flags.BoolVar(&config.migrateOnly, "migrate-only", false, "migrate the database and exit")

	if err := flags.Parse(args); err != nil {
		return config, err
	}
	if config.managementToken != "" && config.managementToken == config.backupToken {
		return config, errors.New("management and backup tokens must be distinct")
	}
	if err := validateAdminConfig(&config); err != nil {
		return config, err
	}
	return config, nil
}

func validateAdminConfig(config *config) error {
	if config.sessionSecret == "" && config.adminOrigin == "" {
		return nil
	}
	if config.managementToken == "" {
		return errors.New("GOTTEM_MANAGEMENT_TOKEN is required when admin UI is configured")
	}
	if len(config.sessionSecret) < 32 {
		return errors.New("GOTTEM_SESSION_SECRET must contain at least 32 bytes")
	}
	if config.adminOrigin == "" {
		return errors.New("GOTTEM_ADMIN_ORIGIN is required when session auth is configured")
	}
	origin, err := url.Parse(config.adminOrigin)
	if err != nil || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" || origin.Opaque != "" {
		return errors.New("GOTTEM_ADMIN_ORIGIN must be an exact HTTP(S) origin without credentials, path, query, or fragment")
	}
	if config.adminOrigin != origin.Scheme+"://"+origin.Host {
		return errors.New("GOTTEM_ADMIN_ORIGIN must be a canonical origin")
	}
	if (origin.Scheme == "https" && origin.Port() == "443") || (origin.Scheme == "http" && origin.Port() == "80") {
		return errors.New("GOTTEM_ADMIN_ORIGIN must omit the default port")
	}
	switch origin.Scheme {
	case "https":
		config.secureCookies = true
	case "http":
		if !isLoopbackHost(origin.Hostname()) {
			return errors.New("HTTP admin origin is allowed only for loopback development")
		}
		config.secureCookies = false
	default:
		return errors.New("GOTTEM_ADMIN_ORIGIN must use HTTPS or loopback HTTP")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
