package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"wikistats/internal/database"
)

// List of client connections to keep streams alive
var (
	clients = make(map[chan []byte]bool)
	mutex   sync.Mutex
)

type Metrics struct {
	ProdConsumed  int
	ProdPersisted int
	ConsConsumed  int
	ConsProcessed int
	ConsFailed    int
}

type Expected struct {
	database.Stats
	Metrics
}

func main() {
	// Stream data for producer to consume
	http.HandleFunc("/stream", streamHandler)
	// Trigger e2e test
	http.HandleFunc("/run-test", testHandler)
	log.Fatal(http.ListenAndServe(":7555", nil))
}

func streamHandler(w http.ResponseWriter, r *http.Request) {
	messageChan := make(chan []byte)
	mutex.Lock()
	clients[messageChan] = true
	mutex.Unlock()

	defer func() {
		mutex.Lock()
		delete(clients, messageChan)
		mutex.Unlock()
	}()

	for msg := range messageChan {
		_, err := fmt.Fprintf(w, "%s\n", msg)
		if err != nil {
			return
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
}

func testHandler(w http.ResponseWriter, r *http.Request) {
	// file query parameter specifies file containing test data to stream
	fileName := r.URL.Query().Get("file")
	if fileName == "" {
		http.Error(w, "Missing 'file' parameter", 400)
		return
	}
	// Log into API to get stats from database
	token, err := login()
	if err != nil {
		http.Error(w, "Login failed: "+err.Error(), 500)
		return
	}
	initialStats, initialMetrics, _ := getSystemState(token)

	file, err := os.Open(fileName)
	if err != nil {
		http.Error(w, "File not found", 404)
		return
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	firstLine, _ := reader.ReadBytes('\n')
	var expected Expected
	json.Unmarshal(firstLine, &expected)

	// Stream file to producer
	broadcast(firstLine)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			broadcast(line)
		}
		if err == io.EOF {
			break
		}
	}

	time.Sleep(15 * time.Second)

	finalStats, finalMetrics, _ := getSystemState(token)

	diffStats := database.Stats{
		Messages: finalStats.Messages - initialStats.Messages,
		Users:    finalStats.Users - initialStats.Users,
		Bots:     finalStats.Bots - initialStats.Bots,
		Servers:  finalStats.Servers - initialStats.Servers,
	}

	diffMetrics := Metrics{
		ProdConsumed:  finalMetrics.ProdConsumed - initialMetrics.ProdConsumed,
		ProdPersisted: finalMetrics.ProdPersisted - initialMetrics.ProdPersisted,
		ConsConsumed:  finalMetrics.ConsConsumed - initialMetrics.ConsConsumed,
		ConsProcessed: finalMetrics.ConsProcessed - initialMetrics.ConsProcessed,
		ConsFailed:    finalMetrics.ConsFailed - initialMetrics.ConsFailed,
	}

	if diffStats == expected.Stats && diffMetrics == expected.Metrics {
		fmt.Fprintln(w, "success")
	} else {
		fmt.Fprintf(w, "failure\nExpected: %+v\nActual: Stats:%+v Metrics:%+v", expected, diffStats, diffMetrics)
	}
}

func broadcast(data []byte) {
	mutex.Lock()
	defer mutex.Unlock()
	for ch := range clients {
		ch <- data
	}
}

func login() (string, error) {
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "admin"})
	resp, err := http.Post("http://api:7000/login", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}
	var res map[string]string
	json.NewDecoder(resp.Body).Decode(&res)
	return res["token"], nil
}

func getSystemState(token string) (database.Stats, Metrics, error) {
	var s database.Stats
	var m Metrics

	req, _ := http.NewRequest("GET", "http://api:7000/stats", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := http.DefaultClient.Do(req)
	json.NewDecoder(resp.Body).Decode(&s)

	m.ProdConsumed = parseMetric("http://producer:2112/metrics", "producer_events_consumed")
	m.ProdPersisted = parseMetric("http://producer:2112/metrics", "producer_events_persisted")
	m.ConsConsumed = parseMetric("http://consumer:2112/metrics", "consumer_events_consumed")
	m.ConsProcessed = parseMetric("http://consumer:2112/metrics", "consumer_events_processed")
	m.ConsFailed = parseMetric("http://consumer:2112/metrics", "consumer_events_failed")

	return s, m, nil
}

func parseMetric(url, metricName string) int {
	resp, err := http.Get(url)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, metricName) {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				val, _ := strconv.Atoi(parts[1])
				return val
			}
		}
	}
	return 0
}
