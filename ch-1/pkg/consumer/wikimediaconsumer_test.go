package consumer

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"wikistats/pkg/database"
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
			consumer := NewWikimediaConsumer("https://stream.wikimedia.org/v2/stream/recentchange")
			consumer.client.Transport = &mockRoundTripper{
				roundTripFunc: tt.setupMock(),
			}
			r, err := consumer.Connect()
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
data: {"user": "alice", "server_url": "en.wikipedia.org", "bot": false}
data: {"user": "bob", "server_url": "fr.wikipedia.org", "bot": true}
`,
			wantErr: false,
			want:    wantState{messages: 2, users: 1, bots: 1, servers: 2},
		},
		{
			name: "Malformed JSON is skipped",
			inputData: `
data: {"user": "alice", "server_url": "en.wikipedia.org", "bot": false}
data: THIS_IS_NOT_JSON
data: {"user": "corey", "server_url": "en.wikipedia.org", "bot": false}
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
data: {"user": "alice", "server_url": "en.wikipedia.org", "bot": false}
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
			consumer := NewWikimediaConsumer("test-url")
			reader := strings.NewReader(tt.inputData)
			err := consumer.Consume(reader, db)
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
