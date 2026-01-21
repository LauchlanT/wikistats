package consumer

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
	"time"
	"wikistats/pkg/database"
	"wikistats/pkg/models"

	"golang.org/x/net/http2"
)

type WikimediaConsumer struct {
	url               string
	client            *http.Client
	reconnectionDelay time.Duration
}

func NewWikimediaConsumer(streamURL string) (*WikimediaConsumer, error) {
	// Configure transport to explicitly be x/net/http2 so errors can be inspected
	transport := &http.Transport{}
	if err := http2.ConfigureTransport(transport); err != nil {
		return nil, err
	}

	return &WikimediaConsumer{
		url: streamURL,
		client: &http.Client{
			Transport: transport,
		},
		reconnectionDelay: 2 * time.Minute,
	}, nil
}

func (c *WikimediaConsumer) Connect(ctx context.Context) (io.Reader, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating http request: %w", err)
	}
	// Wikimedia requires an identifying user agent
	req.Header.Set("User-Agent", "REDspace workshop (lauchlan.toal@redspace.com)")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", c.url, err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("server response: %d %s", resp.StatusCode, resp.Status)
	}
	log.Println("Connected to Wikimedia Stream", c.url)
	return resp.Body, nil
}

func (c *WikimediaConsumer) Consume(ctx context.Context, r io.Reader, db database.Executer) error {
	// Infinite loop to handle reconnections
	for {
		// Scan every line of stream to get change data
		scanner := bufio.NewScanner(r)
		const maxCapacity = 1024 * 1024
		buf := make([]byte, maxCapacity)
		scanner.Buffer(buf, maxCapacity)
		var lastTimestamp string
		for scanner.Scan() {
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
			lastTimestamp = msg.Meta.DT
			db.UpdateDatabase(msg.Meta.ID, msg.User, msg.ServerURL, msg.Bot)
		}
		if err := scanner.Err(); err != nil {
			// Terminate consumer if service is shutting down
			if errors.Is(ctx.Err(), context.Canceled) {
				return ctx.Err()
			}
			var streamError http2.StreamError
			if errors.As(err, &streamError) {
				// Reconnect if the error is just the server cancelling the connection
				if streamError.StreamID == 1 && streamError.Code == http2.ErrCodeCancel {
					if rc, ok := r.(io.ReadCloser); ok {
						rc.Close()
					}
					// Update URL to pull messages since the last read timestamp
					c.url = fmt.Sprintf("%s?since=%s", strings.Split(c.url, "?")[0], url.QueryEscape(lastTimestamp))
					select {
					case <-time.After(c.reconnectionDelay):
						// Delay before reconnecting to avoid disconnects getting faster
					case <-ctx.Done():
						// Service was shut down during the wait
						return ctx.Err()
					}
					r, err = c.Connect(ctx)
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
