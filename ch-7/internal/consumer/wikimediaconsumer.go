package consumer

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
	"wikistats/internal/config"
	"wikistats/internal/database"
	"wikistats/internal/models"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
)

var (
	metricEventsConsumed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "consumer_events_consumed",
		Help: "Total number of events consumed from Redpanda",
	})
	metricEventsProcessed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "consumer_events_processed",
		Help: "Total number of events that successfully processed",
	})
	metricEventsFailed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "consumer_events_failed",
		Help: "Total number of events that failed to process",
	})
)

func init() {
	prometheus.MustRegister(metricEventsConsumed, metricEventsProcessed, metricEventsFailed)
}

type WikimediaConsumer struct {
	rpTimeout     time.Duration
	dbTimeout     time.Duration
	consumerCount int
}

func NewWikimediaConsumer(cfg config.ConsumerConfig) (*WikimediaConsumer, error) {
	return &WikimediaConsumer{
		rpTimeout:     cfg.RPTimeout,
		dbTimeout:     cfg.DBTimeout,
		consumerCount: cfg.ConsumerThreadCount,
	}, nil
}

func (c *WikimediaConsumer) Consume(ctx context.Context, db database.Repository, rp *kgo.Client) error {
	var wg sync.WaitGroup
	for i := 0; i < c.consumerCount; i++ {
		wg.Go(func() {
			for {
				fetches := rp.PollFetches(ctx)
				if ctx.Err() != nil {
					return
				}
				if errs := fetches.Errors(); len(errs) > 0 {
					for _, e := range errs {
						log.Printf("Fetch error: topic=%s, partition=%d, err=%v", e.Topic, e.Partition, e.Err)
					}
					continue
				}
				fetches.EachRecord(func(record *kgo.Record) {
					processed := true
					if err := c.processRecord(ctx, db, record); err != nil {
						log.Printf("Processing error: %v", err)
						processed = false
					}
					if err := rp.CommitRecords(ctx, record); err != nil {
						log.Printf("Error committing record: %v", err)
					} else {
						// Increment metrics only on successful commits to avoid doublecounting
						metricEventsConsumed.Inc()
						if processed {
							metricEventsProcessed.Inc()
						} else {
							metricEventsFailed.Inc()
						}
					}
				})
			}
		})
	}
	wg.Wait()
	return ctx.Err()
}

func (c *WikimediaConsumer) processRecord(ctx context.Context, db database.Repository, record *kgo.Record) error {
	var exported models.Exported
	if err := proto.Unmarshal(record.Value, &exported); err != nil {
		return fmt.Errorf("unmarshal failed: %w", err)
	}

	dbCtx, cancel := context.WithTimeout(ctx, c.dbTimeout)
	defer cancel()

	err := db.UpdateDatabase(dbCtx, database.StatsUpdate{
		Id:     exported.Id,
		User:   exported.User,
		Server: exported.Server,
		IsBot:  exported.IsBot,
	})
	if err != nil {
		return fmt.Errorf("database update failed: %w", err)
	}
	return nil
}
