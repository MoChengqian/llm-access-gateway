package main

import (
	"errors"
	"testing"
	"time"
)

func TestPercentile(t *testing.T) {
	values := []time.Duration{
		10 * time.Millisecond,
		30 * time.Millisecond,
		20 * time.Millisecond,
		40 * time.Millisecond,
	}

	if got := percentile(values, 50); got != 20*time.Millisecond {
		t.Fatalf("expected p50 20ms, got %s", got)
	}
	if got := percentile(values, 95); got != 40*time.Millisecond {
		t.Fatalf("expected p95 40ms, got %s", got)
	}
	if got := percentile(values, 100); got != 40*time.Millisecond {
		t.Fatalf("expected max 40ms, got %s", got)
	}
}

func TestFormatStatusCounts(t *testing.T) {
	got := formatStatusCounts(map[int]int{
		500: 1,
		200: 3,
		429: 2,
	})

	if got != "200=3,429=2,500=1" {
		t.Fatalf("unexpected status counts %s", got)
	}
}

func TestBuildSummary(t *testing.T) {
	sum := buildSummary(report{
		requests:      3,
		concurrency:   2,
		stream:        true,
		totalDuration: 2 * time.Second,
		results: []result{
			{status: 200, latency: 10 * time.Millisecond, ttft: 4 * time.Millisecond, streamChunks: 2},
			{status: 200, latency: 20 * time.Millisecond, ttft: 5 * time.Millisecond, streamChunks: 3},
			{status: 500, latency: 30 * time.Millisecond, err: errors.New("boom")},
		},
	})

	if sum.Success != 2 || sum.Failure != 1 {
		t.Fatalf("unexpected success/failure %#v", sum)
	}
	if sum.StatusCounts[200] != 2 || sum.StatusCounts[500] != 1 {
		t.Fatalf("unexpected status counts %#v", sum.StatusCounts)
	}
	if sum.LatencyP95MS != 30 {
		t.Fatalf("expected latency p95 30ms, got %d", sum.LatencyP95MS)
	}
	if sum.TTFTP95MS != 5 {
		t.Fatalf("expected ttft p95 5ms, got %d", sum.TTFTP95MS)
	}
	if sum.StreamChunks != 5 {
		t.Fatalf("expected 5 stream chunks, got %d", sum.StreamChunks)
	}
	if sum.SampleError == "" {
		t.Fatal("expected sample error to be populated")
	}
}

func TestEvaluateThresholds(t *testing.T) {
	sum := summary{
		Requests:     10,
		Success:      9,
		Failure:      1,
		LatencyP95MS: 120,
		Stream:       true,
		TTFTP95MS:    40,
	}

	if err := evaluateThresholds(sum, config{MinSuccessRate: 0.9, MaxLatencyP95: 200 * time.Millisecond, MaxTTFTP95: 50 * time.Millisecond}); err != nil {
		t.Fatalf("expected thresholds to pass, got %v", err)
	}

	if err := evaluateThresholds(sum, config{MinSuccessRate: 0.95}); err == nil {
		t.Fatal("expected success rate threshold failure")
	}
	if err := evaluateThresholds(sum, config{MaxLatencyP95: 100 * time.Millisecond}); err == nil {
		t.Fatal("expected latency threshold failure")
	}
	if err := evaluateThresholds(summary{Requests: 1, Success: 1, Stream: false}, config{MaxTTFTP95: 10 * time.Millisecond}); err == nil {
		t.Fatal("expected non-stream ttft threshold failure")
	}
}
