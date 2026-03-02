package consumer

import (
	"context"
	"fmt"
	"log"
	"time"
	"wikistats/internal/config"
	"wikistats/internal/database"
	"wikistats/internal/models"

	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
)

type WikimediaConsumer struct {
	rpTimeout time.Duration
	dbTimeout time.Duration
}

func NewWikimediaConsumer(cfg config.ConsumerConfig) (*WikimediaConsumer, error) {
	return &WikimediaConsumer{
		rpTimeout: cfg.RPTimeout,
		dbTimeout: cfg.DBTimeout,
	}, nil
}

func (c *WikimediaConsumer) Consume(ctx context.Context, db database.Repository, rp *kgo.Client) error {
	for {
		fetches := rp.PollFetches(ctx)
		if ctx.Err() != nil {
			break
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
		})
	}
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
