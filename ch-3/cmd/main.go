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
	"sync"
	"syscall"
	"wikistats/pkg/api"
	"wikistats/pkg/config"
	"wikistats/pkg/consumer"
	"wikistats/pkg/database"
	"wikistats/pkg/utils"
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
		if err := utils.LoadEnv(*envFile); err != nil {
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
		Handler:      api.NewRouter(api.NewService(db)),
		ReadTimeout:  cfg.API.ReadTimeout,
		WriteTimeout: cfg.API.WriteTimeout,
		IdleTimeout:  cfg.API.IdleTimeout,
	}

	var wg sync.WaitGroup
	errChan := make(chan error, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("Server starting on port %s", cfg.API.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- fmt.Errorf("server error: %w", err)
			cancel()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Println("Starting consumer")
		stream, err := streamConsumer.Connect(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			errChan <- fmt.Errorf("consumer connection error: %w", err)
			cancel()
			return
		}
		if err = streamConsumer.Consume(ctx, stream, db); err != nil && !errors.Is(err, context.Canceled) {
			errChan <- fmt.Errorf("consumer processing error: %w", err)
			cancel()
		}
	}()

	<-ctx.Done()
	log.Println("Shutdown signal received, stopping services...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.Main.ShutdownTimeout)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}
	wg.Wait()
	close(errChan)
	for e := range errChan {
		if e != nil {
			return e
		}
	}

	log.Println("Application terminated gracefully")
	return nil
}
