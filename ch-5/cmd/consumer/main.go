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
	"wikistats/internal/consumer"
	"wikistats/internal/database"

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
		kgo.SeedBrokers(net.JoinHostPort(cfg.Consumer.RedpandaHost, cfg.Consumer.RedpandaPort)),
		kgo.ConsumerGroup(cfg.Consumer.RedpandaGroup),
		kgo.ConsumeTopics(cfg.Consumer.RedpandaTopic),
		kgo.AutoCommitInterval(cfg.Consumer.RedpandaCommitInterval),
	)
	if err != nil {
		return fmt.Errorf("redpanda connection failed: %w", err)
	}
	defer rp.Close()

	db, err := database.NewGRPCClient(cfg.Database.Host + ":" + cfg.Database.Port)
	if err != nil {
		return fmt.Errorf("database connection failed: %w", err)
	}
	defer func(db database.Repository) {
		err = errors.Join(err, db.Close())
	}(db)

	streamConsumer, err := consumer.NewWikimediaConsumer(cfg.Consumer)
	if err != nil {
		return fmt.Errorf("error initializing consumer: %w", err)
	}

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		log.Println("Starting consumer")

		if err := streamConsumer.Consume(ctx, db, rp); err != nil {
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
