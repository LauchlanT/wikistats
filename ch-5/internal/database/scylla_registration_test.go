//go:build integration

package database

import (
	"fmt"
	"testing"
	"time"
	"wikistats/internal/config"
)

func init() {
	registerImplementation("scylla", func(t *testing.T) (Repository, func()) {
		t.Helper()
		cfg, err := config.LoadFromEnv()
		if err != nil {
			t.Fatalf("Could not create config: %v", err)
		}
		// Use isolated keyspace per test run to avoid collisions
		cfg.Database.Keyspace = fmt.Sprintf("test_%d", time.Now().UnixNano())

		db, err := NewScyllaDatabase(cfg.Database)
		if err != nil {
			t.Fatalf("Failed to connect to ScyllaDB: %v", err)
		}
		if err := db.MigrateDatabase(t.Context()); err != nil {
			t.Fatalf("Failed to migrate database: %v", err)
		}
		if err := db.AddUser(t.Context(), cfg.API.Username, cfg.API.Password); err != nil {
			t.Fatalf("Failed to create user in database: %v", err)
		}

		cleanup := func() {
			if err := db.session.Query("DROP KEYSPACE IF EXISTS " + cfg.Database.Keyspace).Exec(); err != nil {
				t.Errorf("dropping keyspace: %v", err)
			}
			if err := db.Close(); err != nil {
				t.Errorf("closing connection: %v", err)
			}
		}

		return db, cleanup
	})
}
