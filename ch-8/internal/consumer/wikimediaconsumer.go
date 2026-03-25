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

func (c *WikimediaConsumer) Consume(ctx context.Context, db database.Repository, rp RPClient) error {
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
				records := fetches.Records()
				stored, err := c.processRecords(ctx, db, records...)
				if err != nil {
					log.Printf("Processing error: %v", err)
				}
				for i, record := range records {
					if i >= stored {
						metricEventsFailed.Inc()
						continue
					}
					if err := rp.CommitRecords(ctx, record); err != nil {
						log.Printf("Error committing record: %v", err)
					} else {
						metricEventsConsumed.Inc()
						metricEventsProcessed.Inc()
					}
				}
			}
		})
	}
	wg.Wait()
	return ctx.Err()
}

func (c *WikimediaConsumer) processRecords(ctx context.Context, db database.Repository, records ...*kgo.Record) (int, error) {
	exported := make([]models.Exported, len(records))
	request := make([]database.StatsUpdate, len(records))
	for i, record := range records {
		if err := proto.Unmarshal(record.Value, &exported[i]); err != nil {
			return 0, fmt.Errorf("unmarshal failed: %w", err)
		}
		request[i].Id = exported[i].Id
		request[i].User = exported[i].User
		request[i].Server = exported[i].Server
		request[i].IsBot = exported[i].IsBot
	}

	dbCtx, cancel := context.WithTimeout(ctx, c.dbTimeout)
	defer cancel()

	stored, err := db.UpdateDatabase(dbCtx, request...)
	if err != nil {
		return stored, fmt.Errorf("database update failed: %w", err)
	}
	return stored, nil
}
