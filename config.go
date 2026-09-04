package main

import (
	"errors"
	"flag"
	"io"
	"os"
)

type config struct {
	addr            string
	dsn             string
	migrateOnly     bool
	managementToken string
	backupToken     string
}

func parseConfig(args []string) (config, error) {
	flags := flag.NewFlagSet("gottem.link", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	var config config
	config.managementToken = os.Getenv("GOTTEM_MANAGEMENT_TOKEN")
	config.backupToken = os.Getenv("GOTTEM_BACKUP_TOKEN")
	flags.StringVar(&config.addr, "addr", ":8080", "http service address")
	flags.StringVar(&config.dsn, "dsn", "gottem.db", "database file")
	flags.BoolVar(&config.migrateOnly, "migrate-only", false, "migrate the database and exit")

	if err := flags.Parse(args); err != nil {
		return config, err
	}
	if config.managementToken != "" && config.managementToken == config.backupToken {
		return config, errors.New("management and backup tokens must be distinct")
	}
	return config, nil
}
