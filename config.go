package main

import (
	"flag"
	"io"
	"os"
)

type config struct {
	addr            string
	dsn             string
	migrateOnly     bool
	managementToken string
}

func parseConfig(args []string) (config, error) {
	flags := flag.NewFlagSet("gottem.link", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	var config config
	config.managementToken = os.Getenv("GOTTEM_MANAGEMENT_TOKEN")
	flags.StringVar(&config.addr, "addr", ":8080", "http service address")
	flags.StringVar(&config.dsn, "dsn", "gottem.db", "database file")
	flags.BoolVar(&config.migrateOnly, "migrate-only", false, "migrate the database and exit")

	if err := flags.Parse(args); err != nil {
		return config, err
	}
	return config, nil
}
