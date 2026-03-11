package database

import (
	"context"
	"fmt"
	"wikistats/internal/config"
)

type StatsUpdate struct {
	Id     string
	User   string
	Server string
	IsBot  bool
}

type Stats struct {
	Messages int
	Users    int
	Bots     int
	Servers  int
}

type Repository interface {
	UpdateDatabase(ctx context.Context, s StatsUpdate) error
	MigrateDatabase(ctx context.Context) error
	AddUser(ctx context.Context, username string, password string) error
	GetStats(ctx context.Context) (*Stats, error)
	ValidateLogin(ctx context.Context, username string, password string) error
	Close() error
}

func New(cfg config.DatabaseConfig) (Repository, error) {
	switch cfg.Type {
	case "scylla":
		return NewScyllaDatabase(cfg)
	case "inmemory":
		return NewInMemoryDatabase(cfg), nil
	default:
		return nil, fmt.Errorf("unknown database type: %s", cfg.Type)
	}
}
