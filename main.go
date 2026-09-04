package main

import (
	"context"
	"corwinm/gottem.link/db"
	"corwinm/gottem.link/routes"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const shutdownTimeout = 10 * time.Second

func main() {
	config, err := parseConfig(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}

	if config.migrateOnly {
		if err := db.Migrate(config.dsn); err != nil {
			log.Fatal(err)
		}
		return
	}

	database, err := db.Open(config.dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	server := &http.Server{
		Handler:           routes.NewRouterWithBackupToken(database, config.managementToken, config.backupToken),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	listener, err := net.Listen("tcp", config.addr)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Println("Server is listening on " + listener.Addr().String())
	if err := serve(ctx, server, listener); err != nil {
		log.Fatal(err)
	}
}

func serve(ctx context.Context, server *http.Server, listener net.Listener) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return fmt.Errorf("shut down server: %w", err)
	}

	if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
