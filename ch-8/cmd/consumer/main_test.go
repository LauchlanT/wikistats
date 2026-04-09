//go:build unit

package main

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"wikistats/internal/config"
	"wikistats/internal/consumer"
	"wikistats/internal/database"
	"wikistats/internal/models"

	"github.com/twmb/franz-go/pkg/kgo"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"
)

const envFile string = "../../.test_env"

type MockFetcher struct {
	records []*kgo.Record
}

func (f *MockFetcher) Errors() []kgo.FetchError {
	return nil
}

func (f *MockFetcher) Records() []*kgo.Record {
	return f.records
}

type MockClient struct {
	mu             sync.Mutex
	messageCount   int
	committedCount atomic.Uint32
	cancel         context.CancelFunc
}

func (c *MockClient) PollFetches(ctx context.Context) consumer.Fetcher {
	records := make([]*kgo.Record, 10)
	c.mu.Lock()
	start := c.messageCount
	if start == 10000 {
		c.mu.Unlock()
		return &MockFetcher{
			records: nil,
		}
	}
	c.messageCount += 10
	c.mu.Unlock()
	for i := 0; i < 10; i++ {
		records[i] = createRecord(strconv.Itoa(start+i), strconv.Itoa(i), strconv.Itoa(start), false)
	}
	return &MockFetcher{
		records: records,
	}
}

func (c *MockClient) CommitRecords(ctx context.Context, r ...*kgo.Record) error {
	c.committedCount.Add(1)
	if c.committedCount.Load() >= 10000 {
		c.cancel()
	}
	return nil
}

func (c *MockClient) ProduceSync(ctx context.Context, r ...*kgo.Record) kgo.ProduceResults {
	return nil
}

func createRecord(id, user, server string, isBot bool) *kgo.Record {
	exported := &models.Exported{
		Id:     id,
		User:   user,
		Server: server,
		IsBot:  isBot,
	}
	data, _ := proto.Marshal(exported)
	return &kgo.Record{Value: data}
}

// Validate that many consumers can run without data races
func TestConsumers_Race(t *testing.T) {
	if err := config.LoadEnv(envFile); err != nil {
		t.Errorf("Could not load env file: %v", err)
	}
	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("Configuration error: %v", err)
	}
	db := database.NewInMemoryDatabase(cfg.Database)
	if err := db.MigrateDatabase(t.Context()); err != nil {
		t.Fatalf("Migrate error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rp := &MockClient{
		messageCount: 0,
		cancel:       cancel,
	}

	cfg.Consumer.ConsumerCount = 10
	g, gctx := errgroup.WithContext(ctx)
	for range cfg.Consumer.ConsumerCount {
		g.Go(func() error {
			return runConsumer(cfg.Consumer, gctx, db, rp)
		})
	}

	if err := g.Wait(); err != nil {
		t.Fatalf("unexpected error consuming: %v", err)
	}

	stats, err := db.GetStats(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stats.Messages != 10000 {
		t.Errorf("incorrect messages count %d expected %d", stats.Messages, 10000)
	}

	if stats.Users != 10 {
		t.Errorf("incorrect users count %d expected %d", stats.Users, 10)
	}

	if stats.Servers != 1000 {
		t.Errorf("incorrect servers count %d expected %d", stats.Servers, 1000)
	}

	if stats.Bots != 0 {
		t.Errorf("incorrect bot count %d expected %d", stats.Bots, 0)
	}

}
