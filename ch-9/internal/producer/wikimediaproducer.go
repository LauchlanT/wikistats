package producer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"wikistats/internal/config"
	"wikistats/internal/models"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
	"golang.org/x/net/http2"
	"google.golang.org/protobuf/proto"
)

var (
	metricEventsConsumed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "producer_events_consumed",
		Help: "Total number of events consumed from the Wikimedia recent changes stream",
	})
	metricEventsPersisted = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "producer_events_persisted",
		Help: "Total number of events persisted to Redpanda",
	})
)

func init() {
	prometheus.MustRegister(metricEventsConsumed, metricEventsPersisted)
}

type WikimediaProducer struct {
	url               string
	client            *http.Client
	reconnectionDelay time.Duration
	userAgent         string
}

func NewWikimediaProducer(cfg config.ProducerConfig) (*WikimediaProducer, error) {
	// Configure transport to explicitly be x/net/http2 so errors can be inspected
	transport := &http.Transport{}
	if err := http2.ConfigureTransport(transport); err != nil {
		return nil, err
	}

	return &WikimediaProducer{
		url: cfg.StreamURL,
		client: &http.Client{
			Transport: transport,
		},
		reconnectionDelay: cfg.ReconnectionDelay,
		userAgent:         cfg.UserAgent,
	}, nil
}

func (p *WikimediaProducer) Connect(ctx context.Context) (io.Reader, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", p.url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating http request: %w", err)
	}
	// Wikimedia requires an identifying user agent
	req.Header.Set("User-Agent", p.userAgent)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", p.url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server response: %d %s", resp.StatusCode, resp.Status)
	}
	log.Println("Connected to Wikimedia Stream", p.url)
	return resp.Body, nil
}

func (p *WikimediaProducer) Produce(ctx context.Context, r io.Reader, rp recordProducer) error {
	// Infinite loop to handle reconnections
	for {
		// Track in-flight messages to Redpanda with buffer for async errors
		var wg sync.WaitGroup
		errChan := make(chan error, 100)
		go func() {
			for err := range errChan {
				log.Printf("Redpanda produce error: %v", err)
			}
		}()
		// Scan every line of stream to get change data
		scanner := bufio.NewScanner(r)
		const maxCapacity = 1024 * 1024
		buf := make([]byte, maxCapacity)
		scanner.Buffer(buf, maxCapacity)
		var lastTimestamp string
		for scanner.Scan() {
			if ctx.Err() != nil {
				goto shutdown
			}
			line := scanner.Bytes()
			// Identify JSON data lines
			if !bytes.HasPrefix(line, []byte("data: ")) {
				continue
			}
			// Strip the "data: " prefix
			payload := line[6:]
			var msg models.Message
			if err := json.Unmarshal(payload, &msg); err != nil {
				log.Printf("Error parsing JSON: %v", err)
				continue
			}
			metricEventsConsumed.Inc()
			lastTimestamp = msg.Meta.DT
			// Export key fields to protobuf and create record for Redpanda
			exported := &models.Exported{Id: msg.Meta.ID, User: msg.User, Server: msg.ServerURL, IsBot: msg.Bot}
			data, err := proto.Marshal(exported)
			if err != nil {
				log.Printf("Error marshalling exported values: %v", err)
			}
			if exported.Id == "" || exported.User == "" || exported.Server == "" {
				log.Printf("Skipping record with missing values %v", exported)
				continue
			}
			record := &kgo.Record{
				Value: data,
			}
			// Async produce to Redpanda
			wg.Add(1)
			rp.Produce(ctx, record, func(r *kgo.Record, err error) {
				defer wg.Done()
				if err != nil {
					errChan <- fmt.Errorf("failed to produce offset %d: %w", r.Offset, err)
				} else {
					metricEventsPersisted.Inc()
				}
			})
		}
	shutdown:
		// Handle service shutdown
		if ctx.Err() != nil {
			done := make(chan struct{})
			go func() {
				wg.Wait()
				close(done)
				close(errChan)
			}()
			log.Println("Forcing messages flush")
			flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := rp.Flush(flushCtx); err != nil {
				return fmt.Errorf("flushing in-flight messages: %w", err)
			}
			select {
			case <-done:
				log.Println("Messages flushed")
			case <-time.After(10 * time.Second):
				log.Println("Timeout waiting for callbacks")
			}
			return ctx.Err()
		}
		// Handle service error or stream reconnection
		if err := scanner.Err(); err != nil {
			if errors.Is(err, bufio.ErrTooLong) {
				// Skip lines that exceed the buffer and continue reading
				continue
			}
			var streamError http2.StreamError
			if errors.As(err, &streamError) {
				// Reconnect if the error is just the server cancelling the connection
				if streamError.StreamID == 1 && streamError.Code == http2.ErrCodeCancel {
					if rc, ok := r.(io.ReadCloser); ok {
						err := rc.Close()
						if err != nil {
							return fmt.Errorf("closing connection: %w", err)
						}
					}
					// Update URL to pull messages since the last read timestamp
					p.url = fmt.Sprintf("%s?since=%s", strings.Split(p.url, "?")[0], url.QueryEscape(lastTimestamp))
					select {
					case <-time.After(p.reconnectionDelay):
						// Delay before reconnecting to avoid disconnects getting faster
					case <-ctx.Done():
						// Service was shut down during the wait
						goto shutdown
					}
					r, err = p.Connect(ctx)
					if err != nil {
						return fmt.Errorf("reconnecting to stream: %w", err)
					}
					continue
				}
			}
			return fmt.Errorf("scanning stream: %w", err)
		} else {
			// All input consumed
			break
		}
	}
	return nil
}
