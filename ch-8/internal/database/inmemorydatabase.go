package database

import (
	"context"
	"fmt"
	"sync"
	"wikistats/internal/config"

	"golang.org/x/crypto/bcrypt"
)

type InMemoryDatabase struct {
	lock       sync.Mutex
	messages   map[string]struct{}
	users      map[string]struct{}
	bots       map[string]struct{}
	servers    map[string]struct{}
	accounts   map[string]string
	bcryptCost int
}

func NewInMemoryDatabase(cfg config.DatabaseConfig) *InMemoryDatabase {
	return &InMemoryDatabase{bcryptCost: cfg.BcryptCost}
}

func (i *InMemoryDatabase) MigrateDatabase(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("migrating database: %w", ctx.Err())
	default:
	}
	i.lock.Lock()
	defer i.lock.Unlock()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("migrating database: %w", err)
	}

	i.messages = make(map[string]struct{})
	i.users = make(map[string]struct{})
	i.bots = make(map[string]struct{})
	i.servers = make(map[string]struct{})
	i.accounts = make(map[string]string)
	return nil
}

func (i *InMemoryDatabase) Close() error {
	return nil
}

func (i *InMemoryDatabase) AddUser(ctx context.Context, username string, password string) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("adding user: %w", ctx.Err())
	default:
	}
	i.lock.Lock()
	defer i.lock.Unlock()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("adding user: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), i.bcryptCost)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}
	i.accounts[username] = string(hash)
	return nil
}

func (i *InMemoryDatabase) UpdateDatabase(ctx context.Context, u ...StatsUpdate) (int, error) {
	select {
	case <-ctx.Done():
		return 0, fmt.Errorf("updating database: %w", ctx.Err())
	default:
	}
	if len(u) == 0 {
		return 0, nil
	}
	for _, record := range u {
		if record.Id == "" || record.User == "" || record.Server == "" {
			return 0, fmt.Errorf("inserting empty values %+v", u)
		}
	}
	i.lock.Lock()
	defer i.lock.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("updating database: %w", err)
	}
	for _, record := range u {
		i.messages[record.Id] = struct{}{}
		if record.IsBot {
			i.bots[record.User] = struct{}{}
		} else {
			i.users[record.User] = struct{}{}
		}
		i.servers[record.Server] = struct{}{}
	}
	return len(u), nil
}

func (i *InMemoryDatabase) GetStats(ctx context.Context) (*Stats, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("getting stats: %w", ctx.Err())
	default:
	}
	i.lock.Lock()
	defer i.lock.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("getting stats: %w", err)
	}

	return &Stats{
		Messages: len(i.messages),
		Users:    len(i.users),
		Bots:     len(i.bots),
		Servers:  len(i.servers),
	}, nil
}

func (i *InMemoryDatabase) ValidateLogin(ctx context.Context, username string, password string) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("validating login: %w", ctx.Err())
	default:
	}
	i.lock.Lock()
	if err := ctx.Err(); err != nil {
		i.lock.Unlock()
		return fmt.Errorf("validating login: %w", err)
	}
	storedHash, ok := i.accounts[username]
	i.lock.Unlock()

	if !ok {
		return fmt.Errorf("user not found")
	}
	err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password))
	return err
}
