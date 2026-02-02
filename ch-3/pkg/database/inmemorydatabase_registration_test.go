//go:build unit

package database

import (
	"testing"
	"wikistats/pkg/config"
)

func init() {
	registerImplementation("inmemory", func(t *testing.T) (Repository, func()) {
		t.Helper()
		cfg, err := config.LoadFromEnv()
		if err != nil {
			t.Fatalf("Could not create config: %v", err)
		}
		db := NewInMemoryDatabase(cfg.Database)
		if err := db.MigrateDatabase(t.Context()); err != nil {
			t.Fatalf("Failed to migrate database: %v", err)
		}
		if err := db.AddUser(t.Context(), cfg.API.Username, cfg.API.Password); err != nil {
			t.Fatalf("Failed to create user in database: %v", err)
		}
		return db, func() {}
	})
}
