package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRecorderRecordWritesCaptureFile(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "capture.json")
	recorder := newRecorder(outputPath)

	req := httptest.NewRequest("POST", "http://127.0.0.1:4318/v1/traces", nil)
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.ContentLength = 128

	if err := recorder.record(req); err != nil {
		t.Fatalf("record request: %v", err)
	}

	var got capture
	data := mustReadFile(t, outputPath)
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal capture: %v", err)
	}

	if got.RequestCount != 1 {
		t.Fatalf("expected request count 1, got %d", got.RequestCount)
	}
	if got.Method != "POST" {
		t.Fatalf("expected POST method, got %q", got.Method)
	}
	if got.Path != "/v1/traces" {
		t.Fatalf("expected /v1/traces path, got %q", got.Path)
	}
	if got.ContentType != "application/x-protobuf" {
		t.Fatalf("unexpected content type %q", got.ContentType)
	}
	if got.ContentLength != 128 {
		t.Fatalf("expected content length 128, got %d", got.ContentLength)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	return data
}
