package main

import (
	"flag"
	"io"
)

type config struct {
	addr string
	dsn  string
}

func parseConfig(args []string) (config, error) {
	flags := flag.NewFlagSet("gottem.link", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	var config config
	flags.StringVar(&config.addr, "addr", ":8080", "http service address")
	flags.StringVar(&config.dsn, "dsn", "gottem.db", "database file")

	if err := flags.Parse(args); err != nil {
		return config, err
	}
	return config, nil
}
