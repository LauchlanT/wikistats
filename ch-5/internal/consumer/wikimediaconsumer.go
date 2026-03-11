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

	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
)

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
		wg.Add(1)
		go func() {
			defer wg.Done()
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
					if err := c.processRecord(ctx, db, record); err != nil {
						log.Printf("Processing error: %v", err)
					}
					rp.CommitRecords(ctx, record)
				})
			}
		}()
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
