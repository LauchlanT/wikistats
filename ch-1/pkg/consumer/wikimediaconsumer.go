package consumer

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"wikistats/pkg/database"
	"wikistats/pkg/models"
)

type WikimediaConsumer struct {
	url string
}

func NewWikimediaConsumer(streamURL string) *WikimediaConsumer {
	return &WikimediaConsumer{
		url: streamURL,
	}
}

func (c *WikimediaConsumer) Connect() (io.Reader, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", c.url, nil)
	if err != nil {
		return nil, fmt.Errorf("Failed to create request: %w", err)
	}
	// Wikimedia requires an identifying user agent
	req.Header.Set("User-Agent", "REDspace workshop (lauchlan.toal@redspace.com)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Failed to connect: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Error connecting to stream: %d %s", resp.StatusCode, resp.Status)
	}
	fmt.Println("Connected to Wikimedia Stream")
	return resp.Body, nil
}

func (c *WikimediaConsumer) Consume(r io.Reader, db database.Executer) error {
	// Convert from Reader to ReadCloser so the connection can be closed
	if rc, ok := r.(io.ReadCloser); ok {
		defer rc.Close()
	}
	// Scan every line of stream to get change data
	scanner := bufio.NewScanner(r)
	const maxCapacity = 1024 * 1024
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)
	for scanner.Scan() {
		line := scanner.Bytes()
		// Identify JSON data lines by "data: " prefix
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
		db.UpdateDatabase(msg.User, msg.ServerURL, msg.Bot)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}
