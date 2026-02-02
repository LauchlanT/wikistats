package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
	"wikistats/pkg/api"
	"wikistats/pkg/config"
	"wikistats/pkg/consumer"
	"wikistats/pkg/database"
	"wikistats/pkg/utils"
)

func main() {
	// Load environment variables from .env file or specified override
	envFile := flag.String("env", ".env", "override path to environment variables file")
	flag.Parse()
	if *envFile != "" {
		if err := utils.LoadEnv(*envFile); err != nil {
			log.Printf("Could not load env file: %v", err)
		}
	}
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	db, err := database.New(cfg.Database)
	if err != nil {
		log.Fatalf("Database initialization failed: %v", err)
	}
	defer db.Close()
	if err := db.MigrateDatabase(ctx); err != nil {
		log.Fatalf("Error migrating database: %v", err)
	}
	if err := db.AddUser(ctx, cfg.API.Username, cfg.API.Password); err != nil {
		log.Fatalf("Error creating user account: %v", err)
	}

	streamConsumer, err := consumer.NewWikimediaConsumer(cfg.Consumer)
	if err != nil {
		log.Fatalf("Error initializing consumer: %v", err)
	}
	server := &http.Server{
		Addr:         ":" + cfg.API.Port,
		Handler:      api.NewRouter(api.NewService(db)),
		ReadTimeout:  cfg.API.ReadTimeout,
		WriteTimeout: cfg.API.WriteTimeout,
		IdleTimeout:  cfg.API.IdleTimeout,
	}

	var wg sync.WaitGroup

	// Start the API server and the consumer process
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("Server starting on port %s", cfg.API.Port)
		if err := server.ListenAndServe(); err != nil &&
			!errors.Is(err, http.ErrServerClosed) && !errors.Is(err, context.Canceled) {
			log.Printf("Server failed to start: %v", err)
			cancel()
			return
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Println("Starting consumer")
		stream, err := streamConsumer.Connect(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("Consumer failed to start: %v", err)
			cancel()
			return
		}
		if err = streamConsumer.Consume(ctx, stream, db); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("Consumer failed: %v", err)
			cancel()
			return
		}
	}()

	// Gracefully handle shutdown requests and wait for dependencies to terminate
	<-ctx.Done()
	log.Println("Shutdown signal received, stopping services...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}
	wg.Wait()
	log.Println("Application terminated")
}
