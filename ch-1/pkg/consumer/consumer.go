package consumer

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"wikistats/pkg/database"
	"wikistats/pkg/models"
)

const streamURL string = "https://stream.wikimedia.org/v2/stream/recentchange"

func ConsumeMessages(db database.Database) {
	// Connect to Wikimedia stream
	client := &http.Client{}
	req, err := http.NewRequest("GET", streamURL, nil)
	if err != nil {
		log.Fatal(err)
	}
	// Wikimedia requires an identifying user agent
	req.Header.Set("User-Agent", "REDspace workshop (lauchlan.toal@redspace.com)")
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Fatalf("Error connecting to stream: %d %s", resp.StatusCode, resp.Status)
	}
	fmt.Println("Connected to Wikimedia Stream")

	// Scan every line of stream to get change data
	scanner := bufio.NewScanner(resp.Body)
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
		log.Fatal(err)
	}
}
