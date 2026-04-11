package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type capture struct {
	RequestCount  int       `json:"request_count"`
	Method        string    `json:"method"`
	Path          string    `json:"path"`
	ContentType   string    `json:"content_type"`
	ContentLength int64     `json:"content_length"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type recorder struct {
	mu    sync.Mutex
	path  string
	state capture
}

func newRecorder(path string) *recorder {
	return &recorder{path: path}
}

func (r *recorder) record(req *http.Request) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.state.RequestCount++
	r.state.Method = req.Method
	r.state.Path = req.URL.Path
	r.state.ContentType = req.Header.Get("Content-Type")
	r.state.ContentLength = req.ContentLength
	r.state.UpdatedAt = time.Now().UTC()

	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(r.state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.path, append(data, '\n'), 0o644)
}

func main() {
	address := flag.String("address", "127.0.0.1:4318", "listen address")
	output := flag.String("output", "/tmp/lag-otlpstub-capture.json", "capture output path")
	path := flag.String("path", "/v1/traces", "expected OTLP HTTP path")
	flag.Parse()

	recorder := newRecorder(*output)
	mux := http.NewServeMux()
	mux.HandleFunc(*path, func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := recorder.record(req); err != nil {
			http.Error(w, fmt.Sprintf("record request: %v", err), http.StatusInternalServerError)
			return
		}

		_, _ = w.Write([]byte("{}"))
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	server := &http.Server{
		Addr:              *address,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("otlp stub listening on %s, writing capture to %s", *address, *output)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("otlp stub stopped unexpectedly: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("otlp stub shutdown failed: %v", err)
	}
}
