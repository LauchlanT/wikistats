package consumer

import (
	"context"
	"fmt"
	"log"
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
	retryLimit    int
	retryDelay    time.Duration
}

func NewWikimediaConsumer(cfg config.ConsumerConfig) (*WikimediaConsumer, error) {
	return &WikimediaConsumer{
		rpTimeout:     cfg.RPTimeout,
		dbTimeout:     cfg.DBTimeout,
		consumerCount: cfg.ConsumerThreadCount,
		retryLimit:    cfg.RetryLimit,
		retryDelay:    cfg.RetryDelay,
	}, nil
}

func (c *WikimediaConsumer) Consume(ctx context.Context, db database.Repository, rp RPClient) error {
	for {
		fetches := rp.PollFetches(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errs := fetches.Errors(); len(errs) > 0 {
			for _, e := range errs {
				log.Printf("Fetch error: topic=%s, partition=%d, err=%v", e.Topic, e.Partition, e.Err)
			}
			continue
		}
		records := fetches.Records()
		var stored int
		var err error
		retryDelay := c.retryDelay
		for retry := 0; retry < c.retryLimit; retry++ {
			stored, err = c.processRecords(ctx, db, records...)
			if stored == len(records) && err == nil {
				break
			}
			time.Sleep(retryDelay)
			retryDelay *= 2
		}
		if err != nil {
			return fmt.Errorf("processing records: %w", err)
		}
		for i, record := range records {
			if i >= stored {
				break
			}
			if err := rp.CommitRecords(ctx, record); err != nil {
				log.Printf("Error committing record: %v", err)
				// Don't continue committing if there's an error
				// Break and leave offset at the failed commit
				// The message will re-process next time it's polled
				break
			} else {
				metricEventsConsumed.Inc()
				metricEventsProcessed.Inc()
			}
		}
		// An event failed to process even after retries
		// Log the failure and send to DLQ
		if stored < len(records) {
			metricEventsFailed.Inc()
			if err := c.sendToDLQ(ctx, records[stored], rp); err != nil {
				log.Printf("Error sending message to DLQ: %v", err)
			} else if err := rp.CommitRecords(ctx, records[stored]); err != nil {
				log.Printf("Error committing record meant for DLQ: %v", err)
			}
		}
	}
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

func (c *WikimediaConsumer) sendToDLQ(ctx context.Context, record *kgo.Record, rp RPClient) error {
	dlqRecord := &kgo.Record{
		Topic: "wikimedia.dlq",
		Value: record.Value,
		Key:   record.Key,
	}
	dlqRecord.Headers = append(dlqRecord.Headers,
		kgo.RecordHeader{Key: "x-orig-topic", Value: []byte(record.Topic)},
		kgo.RecordHeader{Key: "x-orig-partition", Value: []byte(fmt.Sprint(record.Partition))},
		kgo.RecordHeader{Key: "x-orig-offset", Value: []byte(fmt.Sprint(record.Offset))},
		kgo.RecordHeader{Key: "x-error", Value: []byte("Processing failure")},
	)
	results := rp.ProduceSync(ctx, dlqRecord)
	if err := results.FirstErr(); err != nil {
		return fmt.Errorf("failed to send to DLQ: %w", err)
	}
	return nil
}
