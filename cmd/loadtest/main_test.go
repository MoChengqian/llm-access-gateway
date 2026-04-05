package main

import (
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
