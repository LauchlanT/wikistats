package database

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"wikistats/pkg/utils"
)

const envFile string = "../../.test_env"

type dbImplementation struct {
	name    string
	factory func(t *testing.T) (Repository, func())
}

var dbImplementations []dbImplementation

func registerImplementation(name string, factory func(t *testing.T) (Repository, func())) {
	if err := utils.LoadEnv(envFile); err != nil {
		fmt.Fprintf(os.Stderr, "Could not load env file: %v", err)
	}
	dbImplementations = append(dbImplementations, dbImplementation{name: name, factory: factory})
}

func TestMain(m *testing.M) {
	if len(dbImplementations) == 0 {
		fmt.Fprintln(os.Stderr, "FATAL: No database implementations registered.")
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func assertStats(t *testing.T, db Repository, want Stats) {
	t.Helper()
	stats, err := db.GetStats(t.Context())
	if err != nil {
		t.Errorf("Error getting stats: %v", err)
	}
	if stats.Messages != want.Messages {
		t.Errorf("Messages: got %d, want %d", stats.Messages, want.Messages)
	}
	if stats.Users != want.Users {
		t.Errorf("Users: got %d, want %d", stats.Users, want.Users)
	}
	if stats.Bots != want.Bots {
		t.Errorf("Bots: got %d, want %d", stats.Bots, want.Bots)
	}
	if stats.Servers != want.Servers {
		t.Errorf("Servers: got %d, want %d", stats.Servers, want.Servers)
	}
}

func TestUpdateDatabase(t *testing.T) {
	tests := []struct {
		name    string
		updates []StatsUpdate
		want    Stats
		wantErr bool
	}{
		{
			name: "Single user",
			updates: []StatsUpdate{
				{Id: "msg1", User: "alice", Server: "server1", IsBot: false},
			},
			want:    Stats{Messages: 1, Users: 1, Bots: 0, Servers: 1},
			wantErr: false,
		},
		{
			name: "Single bot",
			updates: []StatsUpdate{
				{Id: "msg1", User: "bob", Server: "server1", IsBot: true},
			},
			want:    Stats{Messages: 1, Users: 0, Bots: 1, Servers: 1},
			wantErr: false,
		},
		{
			name: "Duplicate users, bots, and servers",
			updates: []StatsUpdate{
				{Id: "msg1", User: "alice", Server: "server1", IsBot: false},
				{Id: "msg2", User: "alice", Server: "server1", IsBot: false},
				{Id: "msg3", User: "alice", Server: "server2", IsBot: false},
				{Id: "msg4", User: "bob", Server: "server1", IsBot: true},
				{Id: "msg5", User: "bob", Server: "server2", IsBot: true},
				{Id: "msg6", User: "bob", Server: "server3", IsBot: true},
			},
			want:    Stats{Messages: 6, Users: 1, Bots: 1, Servers: 3},
			wantErr: false,
		},
		{
			name: "Distinct users, bots, and servers",
			updates: []StatsUpdate{
				{Id: "msg1", User: "alice", Server: "server1", IsBot: false},
				{Id: "msg2", User: "bob", Server: "server2", IsBot: true},
				{Id: "msg3", User: "corey", Server: "server3", IsBot: false},
				{Id: "msg4", User: "diane", Server: "server5", IsBot: false},
				{Id: "msg5", User: "elaine", Server: "server8", IsBot: false},
				{Id: "msg6", User: "frank", Server: "server13", IsBot: false},
			},
			want:    Stats{Messages: 6, Users: 5, Bots: 1, Servers: 6},
			wantErr: false,
		},
		{
			name: "Zero values",
			updates: []StatsUpdate{
				{},
			},
			want:    Stats{Messages: 0, Users: 0, Bots: 0, Servers: 0},
			wantErr: true,
		},
		{
			name: "Partial zero values",
			updates: []StatsUpdate{
				{Server: "server1"},
			},
			want:    Stats{Messages: 0, Users: 0, Bots: 0, Servers: 0},
			wantErr: true,
		},
	}
	for _, implementation := range dbImplementations {
		t.Run(implementation.name, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					db, cleanup := implementation.factory(t)
					defer cleanup()
					for _, op := range tt.updates {
						if err := db.UpdateDatabase(t.Context(), StatsUpdate{op.Id, op.User, op.Server, op.IsBot}); (err != nil) != tt.wantErr {
							t.Fatalf("Error updating database: %v", err)
						}
					}
					assertStats(t, db, tt.want)
				})
			}
		})
	}
}

func TestGetStats(t *testing.T) {
	tests := []struct {
		name    string
		updates []StatsUpdate
		want    Stats
	}{
		{
			name:    "Empty database",
			updates: []StatsUpdate{},
			want:    Stats{Messages: 0, Users: 0, Bots: 0, Servers: 0},
		},
		{
			name: "Populated database",
			updates: []StatsUpdate{
				{Id: "msg1", User: "alice", Server: "server1", IsBot: false},
				{Id: "msg2", User: "bob", Server: "server1", IsBot: true},
				{Id: "msg3", User: "corey", Server: "server2", IsBot: false},
			},
			want: Stats{Messages: 3, Users: 2, Bots: 1, Servers: 2},
		},
	}
	for _, implementation := range dbImplementations {
		t.Run(implementation.name, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					db, cleanup := implementation.factory(t)
					defer cleanup()
					for _, op := range tt.updates {
						if err := db.UpdateDatabase(t.Context(), StatsUpdate{op.Id, op.User, op.Server, op.IsBot}); err != nil {
							t.Fatalf("Error updating database: %v", err)
						}
					}
					assertStats(t, db, tt.want)
				})
			}
		})
	}
}

func TestValidateLogin(t *testing.T) {
	tests := []struct {
		name       string
		userDB     string
		passwordDB string
		user       string
		password   string
		errIsNil   bool
	}{
		{
			name:       "Successful login",
			userDB:     "TestAdmin",
			passwordDB: "secretpassword",
			user:       "TestAdmin",
			password:   "secretpassword",
			errIsNil:   true,
		},
		{
			name:       "Invalid login",
			userDB:     "TestAdmin",
			passwordDB: "secretpassword",
			user:       "TestAdmin",
			password:   "wrongpassword",
			errIsNil:   false,
		},
	}
	for _, implementation := range dbImplementations {
		t.Run(implementation.name, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					db, cleanup := implementation.factory(t)
					defer cleanup()
					if err := db.AddUser(t.Context(), tt.userDB, tt.passwordDB); err != nil {
						t.Fatalf("error adding user %s with password %s: %v", tt.userDB, tt.passwordDB, err)
					}
					if err := db.ValidateLogin(t.Context(), tt.user, tt.password); (err == nil) != tt.errIsNil {
						t.Errorf("got %v", err)
					}
				})
			}
		})
	}
}

func TestConcurrentExecution(t *testing.T) {
	tests := []struct {
		name            string
		goroutines      int
		opsPerGoroutine int
		want            Stats
	}{
		{
			name:            "Low Concurrency",
			goroutines:      10,
			opsPerGoroutine: 10,
			want:            Stats{Messages: 100, Users: 100, Bots: 0, Servers: 100},
		},
		{
			name:            "Medium Concurrency",
			goroutines:      100,
			opsPerGoroutine: 100,
			want:            Stats{Messages: 10000, Users: 10000, Bots: 0, Servers: 10000},
		},
		{
			name:            "High Concurrency",
			goroutines:      1000,
			opsPerGoroutine: 1000,
			want:            Stats{Messages: 1000000, Users: 1000000, Bots: 0, Servers: 1000000},
		},
	}

	for _, implementation := range dbImplementations {
		t.Run(implementation.name, func(t *testing.T) {
			for _, tt := range tests {
				if tt.name == "High Concurrency" && implementation.name == "scylla" {
					// Running this on ScyllaDB is unreasonably time-consuming
					continue
				}
				t.Run(tt.name, func(t *testing.T) {
					db, cleanup := implementation.factory(t)
					defer cleanup()
					var wg sync.WaitGroup
					wg.Add(tt.goroutines)
					for i := 0; i < tt.goroutines; i++ {
						go func(routine int) {
							defer wg.Done()
							for j := 0; j < tt.opsPerGoroutine; j++ {
								id := fmt.Sprintf("message-%d-%d", routine, j)
								user := fmt.Sprintf("user-%d-%d", routine, j)
								server := fmt.Sprintf("server-%d-%d", routine, j)
								if err := db.UpdateDatabase(t.Context(), StatsUpdate{id, user, server, false}); err != nil {
									t.Logf("Error updating database: %v", err)
								}
							}
						}(i)
					}
					wg.Wait()
					assertStats(t, db, tt.want)
				})
			}
		})
	}
}
