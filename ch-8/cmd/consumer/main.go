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
	"strings"
	"syscall"
	"time"
	"wikistats/internal/config"
	"wikistats/internal/consumer"
	"wikistats/internal/database"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/twmb/franz-go/pkg/kgo"
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
		return fmt.Errorf("loading configuration: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	promServer := &http.Server{
		Addr:    ":2112",
		Handler: mux,
	}
	go func() {
		if err := promServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("Metrics server error: %v", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelShutdown()
		log.Println("Shutting down metrics server...")
		promServer.Shutdown(shutdownCtx)
	}()

	rp, err := kgo.NewClient(
		kgo.SeedBrokers(strings.Split(cfg.Consumer.RedpandaHost, ",")...),
		kgo.ConsumerGroup(cfg.Consumer.RedpandaGroup),
		kgo.ConsumeTopics(cfg.Consumer.RedpandaTopic),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return fmt.Errorf("connecting to Redpanda: %w", err)
	}
	defer rp.Close()

	db, err := database.NewGRPCClient(net.JoinHostPort(cfg.Database.Host, cfg.Database.Port))
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer func(db database.Repository) {
		err = errors.Join(err, db.Close())
	}(db)

	streamConsumer, err := consumer.NewWikimediaConsumer(cfg.Consumer)
	if err != nil {
		return fmt.Errorf("initializing consumer: %w", err)
	}

	if err := streamConsumer.Consume(ctx, db, rp); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return fmt.Errorf("consuming stream: %w", err)
	}

	log.Println("Application terminated gracefully")
	return nil
}
