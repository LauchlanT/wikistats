package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"wikistats/internal/config"
	"wikistats/internal/producer"

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

	rp, err := kgo.NewClient(
		kgo.SeedBrokers(net.JoinHostPort(cfg.Producer.RedpandaHost, cfg.Producer.RedpandaPort)),
		kgo.DefaultProduceTopic(cfg.Producer.RedpandaTopic),
		kgo.AllowAutoTopicCreation(),
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
		defer closer.Close()
	}

	if err := streamProducer.Produce(ctx, stream, rp); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return fmt.Errorf("producing stream: %w", err)
	}

	log.Println("Application terminated gracefully")
	return nil
}
