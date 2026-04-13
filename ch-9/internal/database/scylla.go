package database

import (
	"context"
	"fmt"
	"sync"
	"time"
	"wikistats/internal/config"

	"github.com/gocql/gocql"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/sync/errgroup"
)

type ScyllaDB struct {
	session     *gocql.Session
	bcryptCost  int
	cachedStats *Stats
	cachedTime  time.Time
	mu          sync.Mutex
}

func NewScyllaDatabase(cfg config.DatabaseConfig) (*ScyllaDB, error) {
	// Connect to Scylla
	cluster := gocql.NewCluster(cfg.Hosts...)
	cluster.Consistency = cfg.Consistency
	cluster.Timeout = cfg.ConnectTimeout
	initSession, err := connectWithRetry(cluster, cfg.ConnectTimeout)
	if err != nil {
		return nil, fmt.Errorf("connecting to ScyllaDB: %w", err)
	}
	defer initSession.Close()

	// Ensure keyspace is initialized
	if err := createKeyspace(initSession, cfg.Keyspace); err != nil {
		return nil, fmt.Errorf("creating keyspace: %w", err)
	}

	// Connect to the application's keyspace
	cluster.Keyspace = cfg.Keyspace
	session, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("connecting to application keyspace: %w", err)
	}

	return &ScyllaDB{
		session:    session,
		bcryptCost: cfg.BcryptCost,
	}, nil
}

func connectWithRetry(cluster *gocql.ClusterConfig, timeout time.Duration) (*gocql.Session, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for connection: %w", ctx.Err())
		case <-ticker.C:
			session, err := cluster.CreateSession()
			if err == nil {
				return session, nil
			}
		}
	}
}

func createKeyspace(session *gocql.Session, keyspace string) error {
	createKeyspaceStmt := fmt.Sprintf(
		"CREATE KEYSPACE IF NOT EXISTS %s WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 3} AND tablets = {'enabled': false};",
		keyspace,
	)
	return session.Query(createKeyspaceStmt).Exec()
}

func (s *ScyllaDB) Close() error {
	if s.session != nil {
		s.session.Close()
	}
	return nil
}

func (s *ScyllaDB) MigrateDatabase(ctx context.Context) error {
	queries := []string{
		"CREATE TABLE IF NOT EXISTS messages (id text PRIMARY KEY);",
		"CREATE TABLE IF NOT EXISTS servers (name text PRIMARY KEY);",
		"CREATE TABLE IF NOT EXISTS users (name text PRIMARY KEY);",
		"CREATE TABLE IF NOT EXISTS bots (name text PRIMARY KEY);",
		"CREATE TABLE IF NOT EXISTS stats (stat text PRIMARY KEY, value counter);",
		"CREATE TABLE IF NOT EXISTS accounts (username text PRIMARY KEY, password text);",
	}
	for _, q := range queries {
		if err := s.session.Query(q).WithContext(ctx).Exec(); err != nil {
			return fmt.Errorf("migration failed for query [%s]: %w", q, err)
		}
	}
	return nil
}

func (s *ScyllaDB) AddUser(ctx context.Context, username string, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.bcryptCost)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}
	err = s.session.Query(`INSERT INTO accounts (username, password) VALUES (?, ?) IF NOT EXISTS`, username, string(hash)).WithContext(ctx).Exec()
	if err != nil {
		return fmt.Errorf("setting password for %s account: %w", username, err)
	}
	return nil
}

func (s *ScyllaDB) UpdateDatabase(ctx context.Context, u ...StatsUpdate) (int, error) {
	if len(u) == 0 {
		return 0, nil
	}
	batch := s.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
	for _, record := range u {
		batch.Query(`INSERT INTO messages (id) VALUES (?)`, record.Id)
		if record.IsBot {
			batch.Query(`INSERT INTO bots (name) VALUES (?)`, record.User)
		} else {
			batch.Query(`INSERT INTO users (name) VALUES (?)`, record.User)
		}
		batch.Query(`INSERT INTO servers (name) VALUES (?)`, record.Server)
	}
	err := s.session.ExecuteBatch(batch)
	if err != nil {
		return 0, fmt.Errorf("executing batch: %w", err)
	}
	return len(u), nil
}

func getCount(session *gocql.Session, table string, count *int) error {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
	if err := session.Query(query).Scan(count); err != nil {
		return fmt.Errorf("getting count of %s: %v", table, err)
	}
	return nil
}

func (s *ScyllaDB) GetStats(ctx context.Context) (*Stats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.cachedTime) < 60*time.Second {
		return s.cachedStats, nil
	}
	var stats Stats
	var g errgroup.Group
	g.Go(func() error { return getCount(s.session, "messages", &stats.Messages) })
	g.Go(func() error { return getCount(s.session, "users", &stats.Users) })
	g.Go(func() error { return getCount(s.session, "bots", &stats.Bots) })
	g.Go(func() error { return getCount(s.session, "servers", &stats.Servers) })
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("getting stat counts: %w", err)
	}
	s.cachedStats = &stats
	s.cachedTime = time.Now()
	return &stats, nil
}

func (s *ScyllaDB) ValidateLogin(ctx context.Context, username string, password string) error {
	var hash string
	err := s.session.Query(`SELECT password FROM accounts WHERE username = ?`, username).WithContext(ctx).Scan(&hash)
	if err != nil {
		return err
	}
	err = bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err
}
