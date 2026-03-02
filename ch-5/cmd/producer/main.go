package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"wikistats/internal/config"
	"wikistats/internal/producer"

	"github.com/twmb/franz-go/pkg/kgo"
	"golang.org/x/sync/errgroup"
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

	rp, err := kgo.NewClient(
		kgo.SeedBrokers(net.JoinHostPort(cfg.Producer.RedpandaHost, cfg.Producer.RedpandaPort)),
		kgo.DefaultProduceTopic(cfg.Producer.RedpandaTopic),
		kgo.AllowAutoTopicCreation(),
	)
	if err != nil {
		return fmt.Errorf("redpanda connection failed: %w", err)
	}
	defer rp.Close()

	streamProducer, err := producer.NewWikimediaProducer(cfg.Producer)
	if err != nil {
		return fmt.Errorf("error initializing producer: %w", err)
	}

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		log.Println("Starting producer")
		stream, err := streamProducer.Connect(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("producer connnection error: %w", err)
		}

		if err := streamProducer.Produce(ctx, stream, rp); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("producer processing error: %w", err)
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return err
	}

	log.Println("Application terminated gracefully")
	return nil
}
