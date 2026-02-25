package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gocql/gocql"
)

type Config struct {
	Main     MainConfig
	Database DatabaseConfig
	API      APIConfig
	Producer ProducerConfig
	Consumer ConsumerConfig
}

type MainConfig struct {
	ShutdownTimeout time.Duration
}

type DatabaseConfig struct {
	Type           string // "scylla" or "inmemory"
	Host           string
	Port           string
	Hosts          []string // ScyllaDB hosts
	Keyspace       string
	Consistency    gocql.Consistency
	ConnectTimeout time.Duration
	BcryptCost     int
}

type APIConfig struct {
	Port          string
	Username      string
	Password      string
	TokenExpiry   time.Duration
	ReadTimeout   time.Duration
	WriteTimeout  time.Duration
	IdleTimeout   time.Duration
	WorkerTimeout time.Duration
}

type ProducerConfig struct {
	RedpandaHost  string
	RedpandaPort  string
	RedpandaTopic string
}

type ConsumerConfig struct {
	StreamURL         string
	ReconnectionDelay time.Duration
	UserAgent         string
	DBTimeout         time.Duration
	RedpandaHost      string
	RedpandaPort      string
	RedpandaTopic     string
	RedpandaGroup     string
}

func LoadFromEnv() (*Config, error) {
	cfg := &Config{
		Main: MainConfig{
			ShutdownTimeout: parseDurationOrDefault("SHUTDOWN_TIMEOUT", 10*time.Second),
		},
		Database: DatabaseConfig{
			Type:           getEnvOrDefault("DATABASE_TYPE", "inmemory"),
			Host:           getEnvOrDefault("DATABASE_HOST", "wikidatabase"),
			Port:           getEnvOrDefault("DATABASE_PORT", "50051"),
			Hosts:          splitEnv("SCYLLA_HOSTS", ","),
			Keyspace:       os.Getenv("SCYLLA_KEYSPACE"),
			Consistency:    parseConsistencyOrDefault("SCYLLA_CONSISTENCY", gocql.Quorum),
			ConnectTimeout: parseDurationOrDefault("SCYLLA_CONNECT_TIMEOUT", 30*time.Second),
			BcryptCost:     parseIntOrDefault("BCRYPT_COST", 14),
		},
		API: APIConfig{
			Port:          getEnvOrDefault("API_PORT", "7000"),
			Username:      getEnvOrDefault("API_USER", "admin"),
			Password:      getEnvOrDefault("API_PASSWORD", "admin"),
			TokenExpiry:   parseDurationOrDefault("API_TOKEN_EXPIRY", 1*time.Hour),
			ReadTimeout:   parseDurationOrDefault("API_READ_TIMEOUT", 10*time.Second),
			WriteTimeout:  parseDurationOrDefault("API_WRITE_TIMEOUT", 10*time.Second),
			IdleTimeout:   parseDurationOrDefault("API_IDLE_TIMEOUT", 120*time.Second),
			WorkerTimeout: parseDurationOrDefault("API_WORKER_TIMEOUT", 5*time.Second),
		},
		Producer: ProducerConfig{
			RedpandaHost:  getEnvOrDefault("REDPANDA_HOST", "redpanda"),
			RedpandaPort:  getEnvOrDefault("REDPANDA_PORT", "9092"),
			RedpandaTopic: getEnvOrDefault("REDPANDA_TOPIC", "wikistats.messages"),
		},
		Consumer: ConsumerConfig{
			StreamURL:         getEnvOrDefault("STREAM_URL", "https://stream.wikimedia.org/v2/stream/recentchange"),
			ReconnectionDelay: parseDurationOrDefault("STREAM_RECONNECTION_DELAY", 120*time.Second),
			UserAgent:         getEnvOrDefault("STREAM_USER_AGENT", "REDspace workshop (lauchlan.toal@redspace.com)"),
			DBTimeout:         parseDurationOrDefault("STREAM_DATABASE_TIMEOUT", 2*time.Second),
			RedpandaHost:      getEnvOrDefault("REDPANDA_HOST", "redpanda"),
			RedpandaPort:      getEnvOrDefault("REDPANDA_PORT", "9092"),
			RedpandaTopic:     getEnvOrDefault("REDPANDA_TOPIC", "wikistats.messages"),
			RedpandaGroup:     getEnvOrDefault("REDPANDA_GROUP", "wikistats-consumers"),
		},
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	if c.Database.Type == "scylla" {
		if len(c.Database.Hosts) == 0 {
			return fmt.Errorf("SCYLLA_HOSTS required when DATABASE_TYPE=scylla")
		}
		if c.Database.Keyspace == "" {
			return fmt.Errorf("SCYLLA_KEYSPACE required when DATABASE_TYPE=scylla")
		}
	}
	if c.Consumer.StreamURL == "" {
		return fmt.Errorf("STREAM_URL is required")
	}
	return nil
}

func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func splitEnv(key, sep string) []string {
	if v := os.Getenv(key); v != "" {
		return strings.Split(v, sep)
	}
	return nil
}

func parseConsistencyOrDefault(key string, defaultVal gocql.Consistency) gocql.Consistency {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	if u, err := strconv.ParseUint(v, 10, 16); err == nil {
		// Only 10 consistency levels are supported by gocql
		if uint16(u) > 10 {
			return defaultVal
		}
		return gocql.Consistency(uint16(u))
	}
	return defaultVal
}

func parseIntOrDefault(key string, defaultVal int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	if i, err := strconv.Atoi(v); err == nil {
		return i
	}
	return defaultVal
}

func parseDurationOrDefault(key string, defaultVal time.Duration) time.Duration {
	s := os.Getenv(key)
	if s == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		// Parse as seconds if no unit provided
		if sec, err := strconv.Atoi(s); err == nil {
			return time.Duration(sec) * time.Second
		}
		return defaultVal
	}
	return d
}
