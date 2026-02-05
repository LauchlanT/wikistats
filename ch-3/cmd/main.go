package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"wikistats/pkg/api"
	"wikistats/pkg/config"
	"wikistats/pkg/consumer"
	"wikistats/pkg/database"

	"golang.org/x/sync/errgroup"
)

func main() {
	if err := run(); err != nil {
		log.Printf("Application error: %v", err)
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

	db, err := database.New(cfg.Database)
	if err != nil {
		return fmt.Errorf("database initialization failed: %w", err)
	}
	defer db.Close()
	if err := db.MigrateDatabase(ctx); err != nil {
		return fmt.Errorf("error migrating database: %w", err)
	}
	if err := db.AddUser(ctx, cfg.API.Username, cfg.API.Password); err != nil {
		return fmt.Errorf("error creating user account: %w", err)
	}

	streamConsumer, err := consumer.NewWikimediaConsumer(cfg.Consumer)
	if err != nil {
		return fmt.Errorf("error initializing consumer: %w", err)
	}

	server := &http.Server{
		Addr:         ":" + cfg.API.Port,
		Handler:      api.NewRouter(api.NewService(cfg.API, db)),
		ReadTimeout:  cfg.API.ReadTimeout,
		WriteTimeout: cfg.API.WriteTimeout,
		IdleTimeout:  cfg.API.IdleTimeout,
	}

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
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
			return nil
		case err := <-serverErr:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("server error during operation: %w", err)
			}
			return nil
		}
	})

	g.Go(func() error {
		log.Println("Starting consumer")
		stream, err := streamConsumer.Connect(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("consumer connnection error: %w", err)
		}

		if err := streamConsumer.Consume(ctx, stream, db); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("consumer processing error: %w", err)
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return err
	}

	log.Println("Application terminated gracefully")
	return nil
}
