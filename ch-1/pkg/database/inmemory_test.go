package database

import (
	"fmt"
	"sync"
	"testing"
)

type updateArgs struct {
	user   string
	server string
	isBot  bool
}

type wantState struct {
	messages int
	users    int
	bots     int
	servers  int
}

func assertStats(t *testing.T, db *InMemory, want wantState) {
	t.Helper()
	gotMessages, gotUsers, gotBots, gotServers := db.GetStats()
	if gotMessages != want.messages {
		t.Errorf("messages: got %d, want %d", gotMessages, want.messages)
	}
	if gotUsers != want.users {
		t.Errorf("users: got %d, want %d", gotUsers, want.users)
	}
	if gotBots != want.bots {
		t.Errorf("bots: got %d, want %d", gotBots, want.bots)
	}
	if gotServers != want.servers {
		t.Errorf("servers: got %d, want %d", gotServers, want.servers)
	}
}

func TestUpdateDatabase(t *testing.T) {
	tests := []struct {
		name    string
		updates []updateArgs
		want    wantState
	}{
		{
			name: "Single user",
			updates: []updateArgs{
				{user: "alice", server: "server1", isBot: false},
			},
			want: wantState{messages: 1, users: 1, bots: 0, servers: 1},
		},
		{
			name: "Single bot",
			updates: []updateArgs{
				{user: "bob", server: "server1", isBot: true},
			},
			want: wantState{messages: 1, users: 0, bots: 1, servers: 1},
		},
		{
			name: "Duplicate users, bots, and servers",
			updates: []updateArgs{
				{user: "alice", server: "server1", isBot: false},
				{user: "alice", server: "server1", isBot: false},
				{user: "alice", server: "server2", isBot: false},
				{user: "bob", server: "server1", isBot: true},
				{user: "bob", server: "server2", isBot: true},
				{user: "bob", server: "server3", isBot: true},
			},
			want: wantState{messages: 6, users: 1, bots: 1, servers: 3},
		},
		{
			name: "Distinct users, bots, and servers",
			updates: []updateArgs{
				{user: "alice", server: "server1", isBot: false},
				{user: "bob", server: "server2", isBot: true},
				{user: "corey", server: "server3", isBot: false},
				{user: "diane", server: "server5", isBot: false},
				{user: "elaine", server: "server8", isBot: false},
				{user: "frank", server: "server13", isBot: false},
			},
			want: wantState{messages: 6, users: 5, bots: 1, servers: 6},
		},
		{
			name: "Zero values",
			updates: []updateArgs{
				{},
			},
			want: wantState{messages: 1, users: 1, bots: 0, servers: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := NewInMemory()
			for _, op := range tt.updates {
				db.UpdateDatabase(op.user, op.server, op.isBot)
			}
			assertStats(t, db, tt.want)
		})
	}
}

func TestGetStats(t *testing.T) {
	tests := []struct {
		name    string
		updates []updateArgs
		want    wantState
	}{
		{
			name:    "Empty database",
			updates: []updateArgs{},
			want:    wantState{messages: 0, users: 0, bots: 0, servers: 0},
		},
		{
			name: "Populated database",
			updates: []updateArgs{
				{user: "alice", server: "server1", isBot: false},
				{user: "bob", server: "server1", isBot: true},
				{user: "corey", server: "server2", isBot: false},
			},
			want: wantState{messages: 3, users: 2, bots: 1, servers: 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := NewInMemory()
			for _, op := range tt.updates {
				db.UpdateDatabase(op.user, op.server, op.isBot)
			}
			assertStats(t, db, tt.want)
		})
	}
}

func TestConcurrentExecution(t *testing.T) {
	tests := []struct {
		name            string
		goroutines      int
		opsPerGoroutine int
		want            wantState
	}{
		{
			name:            "Low Concurrency",
			goroutines:      10,
			opsPerGoroutine: 10,
			want:            wantState{messages: 100, users: 100, bots: 0, servers: 1},
		},
		{
			name:            "Medium Concurrency",
			goroutines:      100,
			opsPerGoroutine: 100,
			want:            wantState{messages: 10000, users: 10000, bots: 0, servers: 1},
		},
		{
			name:            "High Concurrency",
			goroutines:      1000,
			opsPerGoroutine: 1000,
			want:            wantState{messages: 1000000, users: 1000000, bots: 0, servers: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := NewInMemory()
			var wg sync.WaitGroup
			wg.Add(tt.goroutines)
			for i := 0; i < tt.goroutines; i++ {
				go func(id int) {
					defer wg.Done()
					for j := 0; j < tt.opsPerGoroutine; j++ {
						user := fmt.Sprintf("user-%d-%d", id, j)
						server := "server1"
						db.UpdateDatabase(user, server, false)
					}
				}(i)
			}
			wg.Wait()
			assertStats(t, db, tt.want)
		})
	}
}
