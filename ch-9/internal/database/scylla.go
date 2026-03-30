package database

import (
	"context"
	"fmt"
	"log"
	"time"
	"wikistats/internal/config"

	"github.com/gocql/gocql"
	"golang.org/x/crypto/bcrypt"
)

type ScyllaDB struct {
	session    *gocql.Session
	bcryptCost int
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

func (s *ScyllaDB) updateRecord(ctx context.Context, u StatsUpdate) error {
	if u.Id == "" || u.User == "" || u.Server == "" {
		return fmt.Errorf("inserting empty values %+v", u)
	}
	var existingValue string
	messageStored, err := s.session.Query(`INSERT INTO messages (id) VALUES (?) IF NOT EXISTS`, u.Id).WithContext(ctx).ScanCAS(&existingValue)
	if err != nil {
		return fmt.Errorf("inserting message %s: %w", u.Id, err)
	}
	if messageStored {
		if err := s.incrementStat(ctx, "messagecount"); err != nil {
			return fmt.Errorf("incrementing message count: %w", err)
		}
	}
	if u.IsBot {
		botStored, err := s.session.Query(`INSERT INTO bots (name) VALUES (?) IF NOT EXISTS`, u.User).WithContext(ctx).ScanCAS(&existingValue)
		if err != nil {
			return fmt.Errorf("inserting bot %s: %w", u.User, err)
		}
		if botStored {
			if err := s.incrementStat(ctx, "botcount"); err != nil {
				return fmt.Errorf("incrementing bot count: %w", err)
			}
		}
	} else {
		userStored, err := s.session.Query(`INSERT INTO users (name) VALUES (?) IF NOT EXISTS`, u.User).WithContext(ctx).ScanCAS(&existingValue)
		if err != nil {
			return fmt.Errorf("inserting user %s: %w", u.User, err)
		}
		if userStored {
			if err := s.incrementStat(ctx, "usercount"); err != nil {
				return fmt.Errorf("incrementing user count: %w", err)
			}
		}
	}
	serverStored, err := s.session.Query(`INSERT INTO servers (name) VALUES (?) IF NOT EXISTS`, u.Server).WithContext(ctx).ScanCAS(&existingValue)
	if err != nil {
		return fmt.Errorf("inserting server %s: %w", u.Server, err)
	}
	if serverStored {
		if err := s.incrementStat(ctx, "servercount"); err != nil {
			return fmt.Errorf("incrementing server count: %w", err)
		}
	}
	return nil
}

func (s *ScyllaDB) UpdateDatabase(ctx context.Context, u ...StatsUpdate) (int, error) {
	if len(u) == 0 {
		return 0, nil
	}
	for _, record := range u {
		if record.Id == "" || record.User == "" || record.Server == "" {
			return 0, fmt.Errorf("inserting empty values %+v", u)
		}
	}
	batch := s.session.NewBatch(gocql.LoggedBatch).WithContext(ctx)
	for _, record := range u {
		batch.Query(`INSERT INTO messages (id) VALUES (?) IF NOT EXISTS`, record.Id)
		if record.IsBot {
			batch.Query(`INSERT INTO bots (name) VALUES (?) IF NOT EXISTS`, record.User)
		} else {
			batch.Query(`INSERT INTO users (name) VALUES (?) IF NOT EXISTS`, record.User)
		}
		batch.Query(`INSERT INTO servers (name) VALUES (?) IF NOT EXISTS`, record.Server)
	}
	var iter gocql.Iter
	applied, _, err := s.session.ExecuteBatchCAS(batch, &iter)
	if err != nil {
		return 0, fmt.Errorf("executing batch: %w", err)
	}

	// If not applied, batch may contain a value already in DB
	if !applied {
		// Try updating each record individually if so
		for i, record := range u {
			err := s.updateRecord(ctx, record)
			if err != nil {
				return i, fmt.Errorf("processing individual records from batch: %w", err)
			}
		}
		return len(u), nil
	}

	for range len(u) {
		// At this point the values are in the DB, so the best we can do is log errors
		//	if the increments fail.
		if err := s.incrementStat(ctx, "messagecount"); err != nil {
			log.Printf("incrementing message count: %v", err)
		}
		if err := s.incrementStat(ctx, "usercount"); err != nil {
			log.Printf("incrementing user count: %v", err)
		}
		if err := s.incrementStat(ctx, "servercount"); err != nil {
			log.Printf("incrementing server count: %v", err)
		}
		if err := s.incrementStat(ctx, "botcount"); err != nil {
			log.Printf("incrementing bot count: %v", err)
		}
	}
	return len(u), nil
}

func (s *ScyllaDB) incrementStat(ctx context.Context, statName string) error {
	err := s.session.Query(`UPDATE stats SET value = value + 1 WHERE stat = ?`, statName).WithContext(ctx).Exec()
	if err != nil {
		return fmt.Errorf("incrementing stat %s: %w", statName, err)
	}
	return nil
}

func (s *ScyllaDB) GetStats(ctx context.Context) (*Stats, error) {
	var statName string
	var statValue int64
	var stat Stats
	iter := s.session.Query(`SELECT stat, value FROM stats`).WithContext(ctx).Iter()
	for iter.Scan(&statName, &statValue) {
		switch statName {
		case "messagecount":
			stat.Messages = int(statValue)
		case "usercount":
			stat.Users = int(statValue)
		case "botcount":
			stat.Bots = int(statValue)
		case "servercount":
			stat.Servers = int(statValue)
		}
	}
	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("closing ScyllaDB iterator: %w", err)
	}
	return &stat, nil
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
