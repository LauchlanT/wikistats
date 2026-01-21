package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
	"wikistats/pkg/api"
	"wikistats/pkg/consumer"
	"wikistats/pkg/database"
)

func main() {
	const streamURL string = "https://stream.wikimedia.org/v2/stream/recentchange"

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	db := database.NewInMemoryDatabase()
	router := api.NewRouter(api.NewService(db))
	streamConsumer, err := consumer.NewWikimediaConsumer(streamURL)
	if err != nil {
		log.Fatalf("Error initializing consumer: %v", err)
	}
	server := &http.Server{
		Addr:         ":7000",
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	var wg sync.WaitGroup

	// Start the API server and the consumer process
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Println("Server starting on port 7000")
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
		start := time.Now()
		stream, err := streamConsumer.Connect(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("Consumer failed to start: %v", err)
			cancel()
			return
		}
		if err = streamConsumer.Consume(ctx, stream, db); err != nil && !errors.Is(err, context.Canceled) {
			log.Println(db.GetStats())
			log.Println(time.Since(start))
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
