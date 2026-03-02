//go:build unit

package consumer

import (
	"context"
	"testing"
	"time"
	"wikistats/internal/config"
	"wikistats/internal/database"
	"wikistats/internal/models"

	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
)

const envFile string = "../../.test_env"

func createRecord(t *testing.T, id, user, server string, isBot bool) *kgo.Record {
	t.Helper()
	exported := &models.Exported{
		Id:     id,
		User:   user,
		Server: server,
		IsBot:  isBot,
	}
	data, err := proto.Marshal(exported)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}
	return &kgo.Record{Value: data}
}

func TestProcessRecord(t *testing.T) {
	tests := []struct {
		name    string
		records []*kgo.Record
		want    database.Stats
	}{
		{
			name: "Valid records",
			records: []*kgo.Record{
				createRecord(t, "msg1", "alice", "server1", false),
				createRecord(t, "msg2", "bob", "server2", true),
			},
			want: database.Stats{Messages: 2, Users: 1, Bots: 1, Servers: 2},
		},
		{
			name: "Duplicate users and servers",
			records: []*kgo.Record{
				createRecord(t, "msg1", "alice", "server1", false),
				createRecord(t, "msg2", "alice", "server1", false),
				createRecord(t, "msg3", "corey", "server1", false),
			},
			want: database.Stats{Messages: 3, Users: 2, Bots: 0, Servers: 1},
		},
		{
			name:    "Empty",
			records: []*kgo.Record{},
			want:    database.Stats{},
		},
		{
			name: "Invalid protobuf is skipped",
			records: []*kgo.Record{
				createRecord(t, "msg1", "alice", "server1", false),
				{Value: []byte("invalid")},
				createRecord(t, "msg2", "bob", "server2", true),
			},
			want: database.Stats{Messages: 2, Users: 1, Bots: 1, Servers: 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

			consumer := &WikimediaConsumer{
				dbTimeout: 5 * time.Second,
			}

			// Process each record directly
			for _, record := range tt.records {
				err := consumer.processRecord(context.Background(), db, record)
				if err != nil {
					t.Logf("Processing error (expected for invalid records): %v", err)
				}
			}

			stats, err := db.GetStats(t.Context())
			if err != nil {
				t.Fatalf("GetStats error: %v", err)
			}

			if stats.Messages != tt.want.Messages {
				t.Errorf("messages: got %d, want %d", stats.Messages, tt.want.Messages)
			}
			if stats.Users != tt.want.Users {
				t.Errorf("users: got %d, want %d", stats.Users, tt.want.Users)
			}
			if stats.Bots != tt.want.Bots {
				t.Errorf("bots: got %d, want %d", stats.Bots, tt.want.Bots)
			}
			if stats.Servers != tt.want.Servers {
				t.Errorf("servers: got %d, want %d", stats.Servers, tt.want.Servers)
			}
		})
	}
}

func TestProcessRecordTimeout(t *testing.T) {
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

	consumer := &WikimediaConsumer{
		dbTimeout: 1 * time.Nanosecond,
	}
	record := createRecord(t, "msg1", "alice", "server1", false)
	err = consumer.processRecord(context.Background(), db, record)
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
}
