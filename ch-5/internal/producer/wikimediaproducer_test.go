//go:build unit

package producer

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
	"wikistats/internal/config"
	"wikistats/internal/models"

	"github.com/twmb/franz-go/pkg/kgo"
	"golang.org/x/net/http2"
	"google.golang.org/protobuf/proto"
)

const envFile string = "../../.test_env"

type mockRecordProducer struct {
	records []*kgo.Record
	mu      sync.Mutex
	flushed bool
}

func (m *mockRecordProducer) Produce(ctx context.Context, r *kgo.Record, promise func(*kgo.Record, error)) {
	m.mu.Lock()
	m.records = append(m.records, r)
	m.mu.Unlock()
	if promise != nil {
		promise(r, nil)
	}
}

func (m *mockRecordProducer) Flush(ctx context.Context) error {
	m.mu.Lock()
	m.flushed = true
	m.mu.Unlock()
	return nil
}

func (m *mockRecordProducer) getRecords() []*kgo.Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.records
}

// Mock http.RoundTripper to intercept network calls
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
			cfg := config.ProducerConfig{
				StreamURL: "http://test.example.com/stream",
				UserAgent: "TestAgent/1.0",
			}
			producer, err := NewWikimediaProducer(cfg)
			if err != nil {
				t.Fatalf("Error initializing producer: %v", err)
			}
			producer.client.Transport = &mockRoundTripper{
				roundTripFunc: tt.setupMock(),
			}
			r, err := producer.Connect(context.Background())
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

func TestProduce(t *testing.T) {
	tests := []struct {
		name          string
		inputData     string
		wantErr       bool
		expectedCount int
		wantRecords   []models.Exported
	}{
		{
			name: "Valid stream data",
			inputData: `
data: {"meta": { "id": "msg1" }, "user": "alice", "server_url": "server1", "bot": false}
data: {"meta": { "id": "msg2" }, "user": "bob", "server_url": "server2", "bot": true}
`,
			wantErr:       false,
			expectedCount: 2,
			wantRecords: []models.Exported{
				{Id: "msg1", User: "alice", Server: "server1", IsBot: false},
				{Id: "msg2", User: "bob", Server: "server2", IsBot: true},
			},
		},
		{
			name: "Malformed JSON is skipped",
			inputData: `
data: {"meta": { "id": "msg1" }, "user": "alice", "server_url": "server1", "bot": false}
data: THIS_IS_NOT_JSON
data: {"meta": { "id": "msg2" }, "user": "corey", "server_url": "server1", "bot": false}
`,
			wantErr:       false,
			expectedCount: 2,
		},
		{
			name: "Lines without 'data:' prefix are ignored",
			inputData: `
: This is a comment
event: message
id: 12345
data: {"meta": { "id": "msg1" }, "user": "alice", "server_url": "server1", "bot": false}
`,
			wantErr:       false,
			expectedCount: 1,
		},
		{
			name:          "Empty stream",
			inputData:     "",
			wantErr:       false,
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.ProducerConfig{
				StreamURL:         "http://test.example.com/stream",
				UserAgent:         "TestAgent/1.0",
				ReconnectionDelay: 100 * time.Millisecond,
			}
			producer, err := NewWikimediaProducer(cfg)
			if err != nil {
				t.Fatalf("Error initializing producer: %v", err)
			}

			mockClient := &mockRecordProducer{}
			reader := strings.NewReader(tt.inputData)

			// Pass in context with timeout to avoid infinite loop
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			err = producer.Produce(ctx, reader, mockClient)
			if (err != nil) && !tt.wantErr {
				t.Errorf("Produce() unexpected error = %v", err)
			}
			records := mockClient.getRecords()
			if len(records) != tt.expectedCount {
				t.Errorf("produced records count: got %d, want %d", len(records), tt.expectedCount)
			}
			if len(tt.wantRecords) > 0 {
				for i := range tt.wantRecords {
					want := &tt.wantRecords[i]
					if i >= len(records) {
						break
					}
					var got models.Exported
					if err := proto.Unmarshal(records[i].Value, &got); err != nil {
						t.Errorf("Failed to unmarshal record %d: %v", i, err)
						continue
					}
					if got.Id != want.Id || got.User != want.User || got.Server != want.Server || got.IsBot != want.IsBot {
						t.Errorf("record %d: got %+v, want %+v", i, &got, &want)
					}
				}
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
		return nil, errors.New("unexpected call to RoundTrip")
	}
	m.requests = append(m.requests, req)
	resp := m.responses[m.callCount]
	m.callCount++
	return resp, nil
}

func TestReconnect(t *testing.T) {
	cfg := config.ProducerConfig{
		StreamURL:         "http://test.example.com/stream",
		UserAgent:         "TestAgent/1.0",
		ReconnectionDelay: 50 * time.Millisecond,
	}

	producer, err := NewWikimediaProducer(cfg)
	if err != nil {
		t.Fatalf("Error initializing producer: %v", err)
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
	producer.client.Transport = mockTransport

	mockClient := &mockRecordProducer{}

	r, err := producer.Connect(context.Background())
	if err != nil {
		t.Errorf("Got error: %v", err)
	}

	errChan := make(chan error, 1)
	go func() {
		errChan <- producer.Produce(context.Background(), r, mockClient)
	}()

	// Write first message then simulate disconnect
	if _, err := w1.Write([]byte(`data: {"user":"alice","bot":false,"server_url":"server1","meta":{"id":"1","dt":"2025-02-02T2:22:22Z"}}` + "\n\n")); err != nil {
		t.Fatalf("Error writing to w1: %v", err)
	}

	streamError := http2.StreamError{
		StreamID: 1,
		Code:     http2.ErrCodeCancel,
	}
	if err := w1.CloseWithError(streamError); err != nil {
		t.Fatalf("Error closing writer w1: %v", err)
	}

	time.Sleep(cfg.ReconnectionDelay * 5)

	records := mockClient.getRecords()
	if len(records) != 1 {
		t.Errorf("Expected 1 record after first stream, got %d", len(records))
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

	if _, err := w2.Write([]byte(`data: {"user":"bob","bot":true,"server_url":"server2","meta":{"id":"2"}}` + "\n\n")); err != nil {
		t.Fatalf("Error writing to w2: %v", err)
	}
	if err := w2.Close(); err != nil {
		t.Fatalf("Error closing writer w2: %v", err)
	}

	select {
	case produceErr := <-errChan:
		if produceErr != nil && produceErr != io.EOF && !strings.Contains(produceErr.Error(), "StreamError") {
			t.Logf("Produce returned (may be expected error): %v", produceErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Produce did not return after w2 was closed")
	}

	time.Sleep(100 * time.Millisecond)
	records = mockClient.getRecords()
	if len(records) != 2 {
		t.Errorf("Expected 2 total records, got %d", len(records))
	}
}
