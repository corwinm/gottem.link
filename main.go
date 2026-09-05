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
const accessQueueCapacity = 256

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
	var accessWriter *db.AccessWriter
	var localAccessStore *db.DbWrapper
	if config.statsProxyURL != "" && config.managementToken != "" {
		localAccessStore, err = db.OpenAccessStore(config.dsn)
		if err != nil {
			log.Fatal(err)
		}
		defer localAccessStore.Close()
		accessStore, err := db.NewHTTPAccessStore(config.statsProxyURL, config.managementToken, nil)
		if err != nil {
			log.Fatal(err)
		}
		accessWriter = db.NewAccessWriter(accessStore, accessQueueCapacity, func(err error) {
			log.Printf("record redirect access: %v", err)
		})
	}

	server := &http.Server{
		Handler: routes.NewRouterWithAdminStats(database, config.managementToken, config.backupToken, routes.AdminConfig{
			Origin:        config.adminOrigin,
			SessionSecret: config.sessionSecret,
			SecureCookies: config.secureCookies,
		}, accessWriter, localAccessStore),
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
	if err := serve(ctx, server, listener, accessWriter); err != nil {
		log.Fatal(err)
	}
}

func serve(ctx context.Context, server *http.Server, listener net.Listener, accessWriter *db.AccessWriter) error {
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
	if err := shutdown(shutdownCtx, server, accessWriter); err != nil {
		return err
	}

	if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func shutdown(ctx context.Context, server *http.Server, accessWriter *db.AccessWriter) error {
	var writerErr error
	if accessWriter != nil {
		writerErr = accessWriter.Close(ctx)
	}
	serverErr := server.Shutdown(ctx)
	if serverErr != nil {
		_ = server.Close()
	}
	if serverErr != nil {
		return fmt.Errorf("shut down server: %w", serverErr)
	}
	if writerErr != nil {
		return fmt.Errorf("drain access writer: %w", writerErr)
	}
	return nil
}
