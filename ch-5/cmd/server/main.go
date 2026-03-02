package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"wikistats/internal/api"
	"wikistats/internal/config"
	"wikistats/internal/database"
)

func main() {
	if err := run(); err != nil {
		log.Printf("Server error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	envFile := flag.String("env", ".env", "override path to environment variables file")
	flag.Parse()
	if *envFile != "" {
		if err := config.LoadEnv(*envFile); err != nil {
			log.Printf("Could not load env file: %v", err)
		}
	}
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	db, err := database.NewGRPCClient(net.JoinHostPort(cfg.Database.Host, cfg.Database.Port))
	if err != nil {
		return fmt.Errorf("database connection failed: %w", err)
	}
	defer func(db database.Repository) {
		err = errors.Join(err, db.Close())
	}(db)

	server := &http.Server{
		Addr:         ":" + cfg.API.Port,
		Handler:      api.NewRouter(api.NewService(cfg.API, db)),
		ReadTimeout:  cfg.API.ReadTimeout,
		WriteTimeout: cfg.API.WriteTimeout,
		IdleTimeout:  cfg.API.IdleTimeout,
	}

	// Since http.Server doesn't support context, run in a goroutine and capture its error on a channel
	serverErr := make(chan error, 1)
	go func() {
		log.Printf("Server starting on port %s", cfg.API.Port)
		serverErr <- server.ListenAndServe()
	}()

	// Then gracefully shutdown the server if context is canceled, or return an error if unexpected shutdown occurs
	select {
	case <-ctx.Done():
		log.Println("Shutdown signal received, stopping services...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.Main.ShutdownTimeout)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("server forced to shutdown: %w", err)
		}
		if err := <-serverErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server error during shutdown: %w", err)
		}
		log.Println("API server terminated")
		return nil
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server error during operation: %w", err)
		}
		log.Println("API server terminated")
		return nil
	}
}
