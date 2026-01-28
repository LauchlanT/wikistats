package database

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gocql/gocql"
	"golang.org/x/crypto/bcrypt"
)

type ScyllaDB struct {
	Session *gocql.Session
}

func NewScyllaDatabase() (*ScyllaDB, error) {
	// Configure connection settings
	if os.Getenv("SCYLLA_HOSTS") == "" || os.Getenv("SCYLLA_KEYSPACE") == "" {
		return nil, fmt.Errorf("missing value for hosts or keyspace")
	}
	cluster := gocql.NewCluster(strings.Split(os.Getenv("SCYLLA_HOSTS"), ",")...)
	cluster.Consistency = gocql.Quorum
	clusterTimeout, err := strconv.Atoi(os.Getenv("SCYLLA_CLUSTER_TIMEOUT"))
	if err != nil {
		log.Printf("Error converting %s to int, defaulting to 5", os.Getenv("SCYLLA_CLUSTER_TIMEOUT"))
		clusterTimeout = 5
	}
	cluster.Timeout = time.Duration(clusterTimeout) * time.Second

	var session *gocql.Session

	// Connect to Scylla
	maxRetries := 10
	for i := 0; i < maxRetries; i++ {
		session, err = cluster.CreateSession()
		if err == nil {
			log.Printf("Successfully connected to ScyllaDB at %s", os.Getenv("SCYLLA_HOSTS"))
			break
		}
		log.Printf("Waiting for ScyllaDB to be ready... (attempt %d/%d): %v", i+1, maxRetries, err)
		time.Sleep(5 * time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("connecting to ScyllaDB")
	}

	// Ensure keyspace is initialized
	createKeyspaceStmt := fmt.Sprintf(
		"CREATE KEYSPACE IF NOT EXISTS %s WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 3} AND tablets = {'enabled': false};",
		os.Getenv("SCYLLA_KEYSPACE"),
	)
	if err := session.Query(createKeyspaceStmt).Exec(); err != nil {
		return nil, fmt.Errorf("creating keyspace: %w", err)
	}

	// Connect to the application's keyspace
	session.Close()
	cluster.Keyspace = os.Getenv("SCYLLA_KEYSPACE")
	session, err = cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("connecting to application keyspace: %w", err)
	}

	db := &ScyllaDB{Session: session}

	// Ensure DB is initialized
	if err := db.migrate(); err != nil {
		return nil, fmt.Errorf("migrating Scylla DB: %w", err)
	}

	return db, nil
}

func (s *ScyllaDB) Close() {
	if s.Session != nil {
		s.Session.Close()
	}
}

func (s *ScyllaDB) migrate() error {
	queries := []string{
		"CREATE TABLE IF NOT EXISTS messages (id text PRIMARY KEY);",
		"CREATE TABLE IF NOT EXISTS servers (name text PRIMARY KEY);",
		"CREATE TABLE IF NOT EXISTS users (name text PRIMARY KEY);",
		"CREATE TABLE IF NOT EXISTS bots (name text PRIMARY KEY);",
		"CREATE TABLE IF NOT EXISTS stats (stat text PRIMARY KEY, value counter);",
		"CREATE TABLE IF NOT EXISTS accounts (username text PRIMARY KEY, password text);",
	}
	for _, q := range queries {
		if err := s.Session.Query(q).Exec(); err != nil {
			return fmt.Errorf("migration failed for query [%s]: %w", q, err)
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("admin"), 14)
	if err != nil {
		log.Printf("Could not set password for admin account: %v", err)
	} else {
		if err = s.Session.Query(`INSERT INTO accounts (username, password) VALUES (?, ?) IF NOT EXISTS`, "admin", string(hash)).Exec(); err != nil {
			log.Printf("Could not set password for admin account: %v", err)
		}
	}
	return nil
}

func (s *ScyllaDB) UpdateDatabase(id string, user string, server string, isBot bool) {
	var existingValue string
	messageStored, err := s.Session.Query(`INSERT INTO messages (id) VALUES (?) IF NOT EXISTS`, id).ScanCAS(&existingValue)
	if err != nil {
		log.Printf("Error inserting message %s: %v", id, err)
	}
	if messageStored {
		s.incrementStat("messagecount")
	}
	if isBot {
		botStored, err := s.Session.Query(`INSERT INTO bots (name) VALUES (?) IF NOT EXISTS`, user).ScanCAS(&existingValue)
		if err != nil {
			log.Printf("Error inserting bot %s: %v", user, err)
		}
		if botStored {
			s.incrementStat("botcount")
		}
	} else {
		userStored, err := s.Session.Query(`INSERT INTO users (name) VALUES (?) IF NOT EXISTS`, user).ScanCAS(&existingValue)
		if err != nil {
			log.Printf("Error inserting user %s: %v", user, err)
		}
		if userStored {
			s.incrementStat("usercount")
		}
	}
	serverStored, err := s.Session.Query(`INSERT INTO servers (name) VALUES (?) IF NOT EXISTS`, server).ScanCAS(&existingValue)
	if err != nil {
		log.Printf("Error inserting server %s: %v", server, err)
	}
	if serverStored {
		s.incrementStat("servercount")
	}
}

func (s *ScyllaDB) incrementStat(statName string) {
	err := s.Session.Query(`UPDATE stats SET value = value + 1 WHERE stat = ?`, statName).Exec()
	if err != nil {
		log.Printf("Failed to increment stat %s: %v", statName, err)
	}
}

func (s *ScyllaDB) GetStats() (messages int, users int, bots int, servers int) {
	var statName string
	var statValue int64
	iter := s.Session.Query(`SELECT stat, value FROM stats`).Iter()
	for iter.Scan(&statName, &statValue) {
		switch statName {
		case "messagecount":
			messages = int(statValue)
		case "usercount":
			users = int(statValue)
		case "botcount":
			bots = int(statValue)
		case "servercount":
			servers = int(statValue)
		}
	}
	if err := iter.Close(); err != nil {
		log.Printf("Error closing ScyllaDB iterator: %v", err)
	}
	return
}

func (s *ScyllaDB) ValidateLogin(username string, password string) bool {
	var hash string
	err := s.Session.Query(`SELECT password FROM accounts WHERE username = ?`, username).Scan(&hash)
	if err != nil {
		return false
	}
	err = bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}
