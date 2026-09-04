package main

import (
	"net/http"
	"os"
	"time"
)

const defaultHTTPTimeout = 10 * time.Second

func main() {
	client := &http.Client{Timeout: defaultHTTPTimeout}
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, client, nil))
}
