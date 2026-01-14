package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"wikistats/pkg/api"
	"wikistats/pkg/consumer"
	"wikistats/pkg/database"
)

func main() {
	db := database.NewInMemory()
	router := api.NewRouter(api.NewService(db))
	server := &http.Server{
		Addr:         ":7000",
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Start the API server and the consumer process
	go func() {
		log.Println("Server starting on port 7000")
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()
	go func() {
		log.Println("Starting consumer")
		consumer.ConsumeMessages(db)
	}()

	// Gracefully handle shutdown requests
	<-stop
	log.Println("Server shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}
	log.Println("Server shutdown successfully")
}
