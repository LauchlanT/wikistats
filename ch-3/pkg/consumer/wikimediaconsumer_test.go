//go:build unit

package consumer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
	"wikistats/pkg/config"
	"wikistats/pkg/database"

	"golang.org/x/net/http2"
)

const envFile string = "../../.test_env"

// Mock http.RoundTripper to intercept network calls and replace with test responses
type mockRoundTripper struct {
	roundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTripFunc(req)
}

func TestConnect(t *testing.T) {
	tests := []struct {
		name      string
		setupMock func() func(req *http.Request) (*http.Response, error)
		wantErr   bool
	}{
		{
			name: "Successful connection (200 OK)",
			setupMock: func() func(req *http.Request) (*http.Response, error) {
				return func(req *http.Request) (*http.Response, error) {
					if req.Header.Get("User-Agent") == "" {
						return nil, errors.New("missing user agent")
					}
					return &http.Response{
						StatusCode: 200,
						Body:       io.NopCloser(strings.NewReader("OK")),
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "Error connecting",
			setupMock: func() func(req *http.Request) (*http.Response, error) {
				return func(req *http.Request) (*http.Response, error) {
					return nil, errors.New("connection refused")
				}
			},
			wantErr: true,
		},
		{
			name: "Server error (500)",
			setupMock: func() func(req *http.Request) (*http.Response, error) {
				return func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: 500,
						Status:     "500 Internal Server Error",
						Body:       io.NopCloser(strings.NewReader("")),
					}, nil
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := config.LoadEnv(envFile); err != nil {
				t.Errorf("Could not load env file: %v", err)
			}
			cfg, err := config.LoadFromEnv()
			if err != nil {
				t.Fatalf("Configuration error: %v", err)
			}
			consumer, err := NewWikimediaConsumer(cfg.Consumer)
			if err != nil {
				t.Fatalf("Error initializing consumer: %v", err)
			}
			consumer.client.Transport = &mockRoundTripper{
				roundTripFunc: tt.setupMock(),
			}
			r, err := consumer.Connect(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("Connect() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && r == nil {
				t.Error("Connect() returned nil reader on success")
			}
		})
	}
}

func TestConsume(t *testing.T) {
	tests := []struct {
		name      string
		inputData string
		wantErr   bool
		want      database.Stats
	}{
		{
			name: "Valid stream data",
			inputData: `
data: {"meta": { "id": "msg1" }, "user": "alice", "server_url": "server1", "bot": false}
data: {"meta": { "id": "msg2" }, "user": "bob", "server_url": "server2", "bot": true}
`,
			wantErr: false,
			want:    database.Stats{Messages: 2, Users: 1, Bots: 1, Servers: 2},
		},
		{
			name: "Malformed JSON is skipped",
			inputData: `
data: {"meta": { "id": "msg1" }, "user": "alice", "server_url": "server1", "bot": false}
data: THIS_IS_NOT_JSON
data: {"meta": { "id": "msg2" }, "user": "corey", "server_url": "server1", "bot": false}
`,
			wantErr: false,
			want:    database.Stats{Messages: 2, Users: 2, Bots: 0, Servers: 1},
		},
		{
			name: "Lines without 'data:' prefix are ignored",
			inputData: `
: This is a comment
event: message
id: 12345
data: {"meta": { "id": "msg1" }, "user": "alice", "server_url": "server1", "bot": false}
`,
			wantErr: false,
			want:    database.Stats{Messages: 1, Users: 1, Bots: 0, Servers: 1},
		},
		{
			name:      "Empty stream",
			inputData: "",
			wantErr:   false,
			want:      database.Stats{Messages: 0, Users: 0, Bots: 0, Servers: 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := config.LoadEnv(envFile); err != nil {
				t.Errorf("Could not load env file: %v", err)
			}
			cfg, err := config.LoadFromEnv()
			if err != nil {
				t.Fatalf("Configuration error: %v", err)
			}
			db := database.NewInMemoryDatabase(cfg.Database)
			if err := db.MigrateDatabase(t.Context()); err != nil {
				t.Fatalf("Error migrating database: %v", err)
			}
			cfg.Consumer.StreamURL = "non-functional-URL"
			consumer, err := NewWikimediaConsumer(cfg.Consumer)
			if err != nil {
				t.Fatalf("Error initializing consumer: %v", err)
			}
			reader := strings.NewReader(tt.inputData)
			err = consumer.Consume(context.Background(), reader, db)
			if (err != nil) != tt.wantErr {
				t.Errorf("Consume() error = %v, wantErr %v", err, tt.wantErr)
			}
			stats, err := db.GetStats(t.Context())
			if err != nil {
				t.Errorf("Error getting stats: %v", err)
			}
			if stats.Messages != tt.want.Messages {
				t.Errorf("messages: got %d, want %d", stats.Messages, tt.want.Messages)
			}
			if stats.Users != tt.want.Users {
				t.Errorf("users: got %d, want %d", stats.Users, tt.want.Users)
			}
			if stats.Bots != tt.want.Bots {
				t.Errorf("bots: got %d, want %d", stats.Bots, tt.want.Bots)
			}
			if stats.Servers != tt.want.Servers {
				t.Errorf("servers: got %d, want %d", stats.Servers, tt.want.Servers)
			}
		})
	}
}

type SequentialMockTransport struct {
	lock      sync.Mutex
	responses []*http.Response
	requests  []*http.Request
	callCount int
}

func (m *SequentialMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.callCount >= len(m.responses) {
		return nil, fmt.Errorf("unexpected call to RoundTrip")
	}
	m.requests = append(m.requests, req)
	resp := m.responses[m.callCount]
	m.callCount++
	return resp, nil
}

func TestReconnect(t *testing.T) {
	if err := config.LoadEnv(envFile); err != nil {
		t.Errorf("Could not load env file: %v", err)
	}
	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("Configuration error: %v", err)
	}

	consumer, err := NewWikimediaConsumer(cfg.Consumer)
	if err != nil {
		t.Fatalf("Error initializing consumer: %v", err)
	}
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()
	mockTransport := &SequentialMockTransport{
		responses: []*http.Response{
			{
				StatusCode: http.StatusOK,
				Body:       r1,
			},
			{
				StatusCode: http.StatusOK,
				Body:       r2,
			},
		},
	}
	consumer.client.Transport = mockTransport
	consumer.reconnectionDelay = cfg.Consumer.ReconnectionDelay
	r, err := consumer.Connect(context.Background())
	if err != nil {
		t.Errorf("Got error: %v", err)
	}
	db := database.NewInMemoryDatabase(cfg.Database)
	if err := db.MigrateDatabase(t.Context()); err != nil {
		t.Fatalf("Error migrating database: %v", err)
	}
	errChan := make(chan error, 1)
	go func() {
		errChan <- consumer.Consume(context.Background(), r, db)
	}()
	w1.Write([]byte(`data: {"user":"alice","bot":false,"server_url":"server1","meta":{"id":"1","dt":"2025-02-02T2:22:22Z"}}` + "\n\n"))
	streamError := http2.StreamError{
		StreamID: 1,
		Code:     http2.ErrCodeCancel,
	}
	w1.CloseWithError(streamError)
	time.Sleep(cfg.Consumer.ReconnectionDelay * 5)
	stats, err := db.GetStats(t.Context())
	if err != nil {
		t.Errorf("Error getting stats: %v", err)
	}
	if stats.Messages != 1 {
		t.Errorf("Message not stored from w1")
	}
	mockTransport.lock.Lock()
	if len(mockTransport.requests) < 2 {
		t.Fatalf("Expected 2 HTTP requests (initial connect + reconnect)")
	}
	reconnectURL := mockTransport.requests[1].URL.String()
	mockTransport.lock.Unlock()
	expectedSuffix := url.QueryEscape("2025-02-02T2:22:22Z")
	if !strings.HasSuffix(reconnectURL, expectedSuffix) {
		t.Errorf("Timestamp not correctly generated in reconnect URL.\nGot: %s\nExpected suffix: %s", reconnectURL, expectedSuffix)
	}
	w2.Write([]byte(`data: {"user":"bob","bot":true,"server_url":"server2","meta":{"id":"2"}}` + "\n\n"))
	w2.Close()
	select {
	case consumeErr := <-errChan:
		if consumeErr != nil && consumeErr != io.EOF && !strings.Contains(consumeErr.Error(), "StreamError") {
			t.Logf("Consume returned (may be expected error): %v", consumeErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Consume did not return after w2 was closed")
	}
	time.Sleep(100 * time.Millisecond)
	stats, err = db.GetStats(t.Context())
	if err != nil {
		t.Errorf("Error getting stats: %v", err)
	}
	if stats.Messages != 2 {
		t.Errorf("Message not stored from w2")
	}
}
