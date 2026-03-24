//go:build integration

package producer

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"wikistats/internal/config"

	"github.com/twmb/franz-go/pkg/kgo"
)

const envFile string = "../../.test_env"

// Wrap the client to record successful deliveries
type clientWrapper struct {
	client  *kgo.Client
	records []*kgo.Record
	mu      sync.Mutex
}

func (c *clientWrapper) Produce(ctx context.Context, r *kgo.Record, promise func(*kgo.Record, error)) {
	produceCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results := c.client.ProduceSync(produceCtx, r)
	err := results.FirstErr()
	if err == nil {
		c.mu.Lock()
		c.records = append(c.records, r)
		c.mu.Unlock()
	} else {
		fmt.Fprintf(os.Stderr, "Redpanda produce error: %v\n", err)
	}
	if promise != nil {
		promise(r, err)
	}
}

func (c *clientWrapper) Flush(ctx context.Context) error {
	return c.client.Flush(ctx)
}

func (c *clientWrapper) getRecords() []*kgo.Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]*kgo.Record, len(c.records))
	copy(result, c.records)
	return result
}

func init() {
	registerProducerImplementation("redpanda", func(t *testing.T) (recordProducer, func() []*kgo.Record, func()) {
		t.Helper()
		if err := config.LoadEnv(envFile); err != nil {
			t.Errorf("Could not load env file: %v", err)
		}
		cfg, err := config.LoadFromEnv()
		if err != nil {
			t.Fatalf("Configuration error: %v", err)
		}
		// Create the client with the unique topic as default
		topic := fmt.Sprintf("test-wikimedia-%d", time.Now().UnixNano())
		client, err := kgo.NewClient(
			kgo.SeedBrokers(strings.Split(cfg.Producer.RedpandaHost, ",")...),
			kgo.DefaultProduceTopic(topic),
			kgo.AllowAutoTopicCreation(),
			kgo.RequiredAcks(kgo.AllISRAcks()),
		)
		if err != nil {
			t.Fatalf("Failed to create Redpanda client: %v", err)
		}
		wrapper := &clientWrapper{
			client:  client,
			records: make([]*kgo.Record, 0),
		}
		getRecords := func() []*kgo.Record {
			flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := wrapper.Flush(flushCtx); err != nil {
				t.Logf("Warning: failed to flush messages: %v", err)
			}

			return wrapper.getRecords()
		}
		cleanup := func() {
			client.Close()
		}
		return wrapper, getRecords, cleanup
	})
}
