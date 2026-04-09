package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"wikistats/internal/config"
	"wikistats/internal/producer"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

func main() {
	if err := run(); err != nil {
		log.Printf("Server error: %v", err)
		os.Exit(1)
	}
}

func configureTopic(ctx context.Context, cfg *config.Config) error {
	// Create temporary admin client
	client, err := kgo.NewClient(
		kgo.SeedBrokers(strings.Split(cfg.Producer.RedpandaHost, ",")...),
		kgo.RequestRetries(3),
	)
	if err != nil {
		return err
	}
	defer client.Close()
	admin := kadm.NewClient(client)
	defer admin.Close()

	resp, err := admin.CreateTopics(
		ctx,
		cfg.Producer.TopicPartitions,
		cfg.Producer.TopicReplication,
		map[string]*string{
			"retention.ms": &cfg.Producer.TopicRetention,
		},
		cfg.Producer.RedpandaTopic,
		cfg.Producer.DLQTopic,
	)
	if err != nil {
		return err
	}

	topicResp := resp[cfg.Producer.RedpandaTopic]
	if topicResp.Err != nil {
		if errors.Is(topicResp.Err, kerr.TopicAlreadyExists) {
			return nil
		}
		return topicResp.Err
	}

	return nil
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
	active := false

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if active {
			w.WriteHeader(http.StatusOK)
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
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
		if err := promServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("Error shutting down metrics server: %v", err)
		}
	}()

	if err := configureTopic(ctx, cfg); err != nil {
		return fmt.Errorf("creating topic: %w", err)
	}

	rp, err := kgo.NewClient(
		kgo.SeedBrokers(strings.Split(cfg.Producer.RedpandaHost, ",")...),
		kgo.DefaultProduceTopic(cfg.Producer.RedpandaTopic),
	)
	if err != nil {
		return fmt.Errorf("connecting to Redpanda: %w", err)
	}
	defer rp.Close()

	streamProducer, err := producer.NewWikimediaProducer(cfg.Producer)
	if err != nil {
		return fmt.Errorf("initializing producer: %w", err)
	}

	stream, err := streamProducer.Connect(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return fmt.Errorf("connecting to stream: %w", err)
	}
	if closer, ok := stream.(io.ReadCloser); ok {
		defer func(closer io.ReadCloser) {
			err = errors.Join(err, closer.Close())
		}(closer)
	}

	active = true
	defer func() { active = false }()
	if err := streamProducer.Produce(ctx, stream, rp); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return fmt.Errorf("producing stream: %w", err)
	}

	log.Println("Application terminated gracefully")
	return nil
}
