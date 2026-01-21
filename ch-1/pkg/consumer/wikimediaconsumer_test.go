package consumer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
	"wikistats/pkg/database"

	"golang.org/x/net/http2"
)

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
			consumer, err := NewWikimediaConsumer("https://stream.wikimedia.org/v2/stream/recentchange")
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
	type wantState struct {
		messages int
		users    int
		bots     int
		servers  int
	}

	tests := []struct {
		name      string
		inputData string
		wantErr   bool
		want      wantState
	}{
		{
			name: "Valid stream data",
			inputData: `
data: {"meta": { "id": "msg1" }, "user": "alice", "server_url": "server1", "bot": false}
data: {"meta": { "id": "msg2" }, "user": "bob", "server_url": "server2", "bot": true}
`,
			wantErr: false,
			want:    wantState{messages: 2, users: 1, bots: 1, servers: 2},
		},
		{
			name: "Malformed JSON is skipped",
			inputData: `
data: {"meta": { "id": "msg1" }, "user": "alice", "server_url": "server1", "bot": false}
data: THIS_IS_NOT_JSON
data: {"meta": { "id": "msg2" }, "user": "corey", "server_url": "server1", "bot": false}
`,
			wantErr: false,
			want:    wantState{messages: 2, users: 2, bots: 0, servers: 1},
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
			want:    wantState{messages: 1, users: 1, bots: 0, servers: 1},
		},
		{
			name:      "Empty stream",
			inputData: "",
			wantErr:   false,
			want:      wantState{messages: 0, users: 0, bots: 0, servers: 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := database.NewInMemoryDatabase()
			consumer, err := NewWikimediaConsumer("test-url")
			if err != nil {
				t.Fatalf("Error initializing consumer: %v", err)
			}
			reader := strings.NewReader(tt.inputData)
			err = consumer.Consume(context.Background(), reader, db)
			if (err != nil) != tt.wantErr {
				t.Errorf("Consume() error = %v, wantErr %v", err, tt.wantErr)
			}
			gotMessages, gotUsers, gotBots, gotServers := db.GetStats()
			if gotMessages != tt.want.messages {
				t.Errorf("messages: got %d, want %d", gotMessages, tt.want.messages)
			}
			if gotUsers != tt.want.users {
				t.Errorf("users: got %d, want %d", gotUsers, tt.want.users)
			}
			if gotBots != tt.want.bots {
				t.Errorf("bots: got %d, want %d", gotBots, tt.want.bots)
			}
			if gotServers != tt.want.servers {
				t.Errorf("servers: got %d, want %d", gotServers, tt.want.servers)
			}
		})
	}
}

type SequentialMockTransport struct {
	responses []*http.Response
	callCount int
}

func (m *SequentialMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if m.callCount >= len(m.responses) {
		return nil, fmt.Errorf("unexpected call to RoundTrip")
	}
	resp := m.responses[m.callCount]
	m.callCount++
	return resp, nil
}

func TestReconnect(t *testing.T) {
	consumer, err := NewWikimediaConsumer("https://stream.wikimedia.org/v2/stream/recentchange")
	if err != nil {
		t.Fatalf("Error initializing consumer: %v", err)
	}
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()
	consumer.client.Transport = &SequentialMockTransport{
		responses: []*http.Response{
			{
				StatusCode: 200,
				Body:       r1,
			},
			{
				StatusCode: 200,
				Body:       r2,
			},
		},
	}
	consumer.reconnectionDelay = 10 * time.Millisecond
	r, err := consumer.Connect(context.Background())
	if err != nil {
		t.Errorf("Got error: %v", err)
	}
	db := database.NewInMemoryDatabase()
	go func() {
		if err := consumer.Consume(context.Background(), r, db); err != nil {
			t.Errorf("Error consuming: %v", err)
		}
	}()
	w1.Write([]byte(`data: {"user":"alice","bot":false,"meta":{"id":"1","dt":"2025-02-02T2:22:22Z"}}` + "\n\n"))
	streamError := http2.StreamError{
		StreamID: 1,
		Code:     http2.ErrCodeCancel,
	}
	w1.CloseWithError(streamError)
	time.Sleep(100 * time.Millisecond)
	messages, _, _, _ := db.GetStats()
	if messages != 1 {
		t.Errorf("Message not stored from w1")
	}
	if !strings.HasSuffix(consumer.url, url.QueryEscape("2025-02-02T2:22:22Z")) {
		t.Errorf("Timestamp not correctly generated %s", consumer.url)
	}
	w2.Write([]byte(`data: {"user":"bob","bot":true,"meta":{"id":"2"}}` + "\n\n"))
	w2.Close()
	time.Sleep(100 * time.Millisecond)
	messages, _, _, _ = db.GetStats()
	if messages != 2 {
		t.Errorf("Message not stored from w2")
	}
}
