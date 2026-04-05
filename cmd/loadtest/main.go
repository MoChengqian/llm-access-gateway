package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type config struct {
	URL         string
	APIKey      string
	Model       string
	Message     string
	Requests    int
	Concurrency int
	Stream      bool
	Timeout     time.Duration
}

type result struct {
	status       int
	latency      time.Duration
	ttft         time.Duration
	streamChunks int
	err          error
}

type report struct {
	requests      int
	concurrency   int
	stream        bool
	totalDuration time.Duration
	results       []result
}

func main() {
	cfg := parseFlags()

	if cfg.APIKey == "" {
		fmt.Fprintln(os.Stderr, "-auth-key is required")
		os.Exit(1)
	}
	if cfg.Requests <= 0 || cfg.Concurrency <= 0 {
		fmt.Fprintln(os.Stderr, "-requests and -concurrency must be > 0")
		os.Exit(1)
	}

	rep, err := runLoadTest(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load test failed: %v\n", err)
		os.Exit(1)
	}

	printReport(rep)
}

func parseFlags() config {
	var cfg config
	flag.StringVar(&cfg.URL, "url", "http://127.0.0.1:8080/v1/chat/completions", "gateway endpoint")
	flag.StringVar(&cfg.APIKey, "auth-key", "", "bearer api key")
	flag.StringVar(&cfg.Model, "model", "", "model override")
	flag.StringVar(&cfg.Message, "message", "hello from loadtest", "user message")
	flag.IntVar(&cfg.Requests, "requests", 20, "total requests")
	flag.IntVar(&cfg.Concurrency, "concurrency", 4, "parallel workers")
	flag.BoolVar(&cfg.Stream, "stream", false, "enable streaming requests")
	flag.DurationVar(&cfg.Timeout, "timeout", 30*time.Second, "per-request timeout")
	flag.Parse()
	return cfg
}

func runLoadTest(cfg config) (report, error) {
	client := &http.Client{}
	results := make([]result, cfg.Requests)

	var next uint64
	started := time.Now()
	var wg sync.WaitGroup
	for worker := 0; worker < cfg.Concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				index := int(atomic.AddUint64(&next, 1)) - 1
				if index >= cfg.Requests {
					return
				}
				results[index] = executeOnce(client, cfg)
			}
		}()
	}
	wg.Wait()

	return report{
		requests:      cfg.Requests,
		concurrency:   cfg.Concurrency,
		stream:        cfg.Stream,
		totalDuration: time.Since(started),
		results:       results,
	}, nil
}

func executeOnce(client *http.Client, cfg config) result {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	body, err := buildRequestBody(cfg)
	if err != nil {
		return result{err: err}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return result{err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return result{err: err}
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if cfg.Stream {
		return consumeStream(resp, started)
	}
	return consumeJSON(resp, started)
}

func buildRequestBody(cfg config) ([]byte, error) {
	payload := map[string]any{
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": cfg.Message,
			},
		},
		"stream": cfg.Stream,
	}
	if strings.TrimSpace(cfg.Model) != "" {
		payload["model"] = cfg.Model
	}
	return json.Marshal(payload)
}

func consumeJSON(resp *http.Response, started time.Time) result {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return result{status: resp.StatusCode, latency: time.Since(started), err: err}
	}
	if resp.StatusCode != http.StatusOK {
		return result{status: resp.StatusCode, latency: time.Since(started), err: fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))}
	}
	if !bytes.Contains(body, []byte(`"object":"chat.completion"`)) {
		return result{status: resp.StatusCode, latency: time.Since(started), err: fmt.Errorf("unexpected response body: %s", strings.TrimSpace(string(body)))}
	}
	return result{status: resp.StatusCode, latency: time.Since(started)}
}

func consumeStream(resp *http.Response, started time.Time) result {
	reader := bufio.NewReader(resp.Body)
	var builder strings.Builder
	chunkCount := 0
	ttft := time.Duration(0)
	sawDone := false

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return result{status: resp.StatusCode, latency: time.Since(started), err: err}
		}

		builder.WriteString(line)
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		if ttft == 0 && data != "" && data != "[DONE]" {
			ttft = time.Since(started)
		}
		if data == "[DONE]" {
			sawDone = true
			continue
		}
		chunkCount++
	}

	latency := time.Since(started)
	body := builder.String()
	if resp.StatusCode != http.StatusOK {
		return result{status: resp.StatusCode, latency: latency, err: fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(body))}
	}
	if !sawDone {
		return result{status: resp.StatusCode, latency: latency, err: fmt.Errorf("stream missing [DONE]: %s", strings.TrimSpace(body))}
	}
	if chunkCount < 1 {
		return result{status: resp.StatusCode, latency: latency, err: fmt.Errorf("stream missing data chunks: %s", strings.TrimSpace(body))}
	}
	return result{status: resp.StatusCode, latency: latency, ttft: ttft, streamChunks: chunkCount}
}

func printReport(rep report) {
	statusCounts := make(map[int]int)
	latencies := make([]time.Duration, 0, len(rep.results))
	ttfts := make([]time.Duration, 0, len(rep.results))
	failures := 0
	totalChunks := 0

	for _, item := range rep.results {
		statusCounts[item.status]++
		if item.latency > 0 {
			latencies = append(latencies, item.latency)
		}
		if item.ttft > 0 {
			ttfts = append(ttfts, item.ttft)
		}
		totalChunks += item.streamChunks
		if item.err != nil {
			failures++
		}
	}

	fmt.Printf("requests=%d concurrency=%d stream=%t total_duration=%s\n", rep.requests, rep.concurrency, rep.stream, rep.totalDuration)
	fmt.Printf("success=%d failure=%d\n", rep.requests-failures, failures)
	fmt.Printf("status_counts=%s\n", formatStatusCounts(statusCounts))
	if len(latencies) > 0 {
		fmt.Printf("latency_p50=%s latency_p95=%s latency_max=%s\n", percentile(latencies, 50), percentile(latencies, 95), percentile(latencies, 100))
	}
	if rep.stream {
		fmt.Printf("stream_chunks_total=%d\n", totalChunks)
		if len(ttfts) > 0 {
			fmt.Printf("ttft_p50=%s ttft_p95=%s ttft_max=%s\n", percentile(ttfts, 50), percentile(ttfts, 95), percentile(ttfts, 100))
		}
	}

	for _, item := range rep.results {
		if item.err != nil {
			fmt.Printf("sample_error=%v\n", item.err)
			break
		}
	}
}

func formatStatusCounts(values map[int]int) string {
	keys := make([]int, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Ints(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%d=%d", key, values[key]))
	}
	return strings.Join(parts, ",")
}

func percentile(values []time.Duration, p int) time.Duration {
	if len(values) == 0 {
		return 0
	}

	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}

	index := (len(sorted)*p - 1) / 100
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}
