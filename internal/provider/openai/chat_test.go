package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/provider"
)

const (
	openAITestContentTypeHeader    = "Content-Type"
	openAITestJSONContentType      = "application/json"
	openAITestEventStreamType      = "text/event-stream"
	openAITestDefaultModel         = "gpt-4.1-mini"
	openAICompletionResponsePrefix = `{"id":"chatcmpl-1","object":"chat.completion","created":123,"model":"`
	openAICompletionResponseSuffix = `","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`
)

func TestCreateChatCompletion(t *testing.T) {
	var authHeader string
	var payload requestPayload

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		w.Header().Set(openAITestContentTypeHeader, openAITestJSONContentType)
		_, _ = w.Write([]byte(openAICompletionResponsePrefix + openAITestDefaultModel + openAICompletionResponseSuffix))
	}))
	defer server.Close()

	p := New(Config{
		BaseURL:      server.URL,
		APIKey:       "test-key",
		DefaultModel: openAITestDefaultModel,
	})

	resp, err := p.CreateChatCompletion(context.Background(), provider.ChatCompletionRequest{
		Messages: []provider.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("create chat completion: %v", err)
	}

	if authHeader != "Bearer test-key" {
		t.Fatalf("expected authorization header, got %s", authHeader)
	}
	if payload.Model != openAITestDefaultModel || payload.Stream {
		t.Fatalf("unexpected request payload %#v", payload)
	}
	if resp.Model != openAITestDefaultModel || resp.Choices[0].Message.Content != "hello" {
		t.Fatalf("unexpected response %#v", resp)
	}
}

func TestStreamChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(openAITestContentTypeHeader, openAITestEventStreamType)
		_, _ = w.Write([]byte(openAIStreamChunk(openAITestDefaultModel, `{"role":"assistant","content":"hel"}`, "")))
		_, _ = w.Write([]byte(openAIStreamChunk(openAITestDefaultModel, `{"content":"lo"}`, "stop")))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p := New(Config{
		BaseURL:      server.URL,
		DefaultModel: openAITestDefaultModel,
	})

	chunks, err := p.StreamChatCompletion(context.Background(), provider.ChatCompletionRequest{
		Messages: []provider.ChatMessage{{Role: "user", Content: "hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("stream chat completion: %v", err)
	}

	var parts []string
	for event := range chunks {
		if event.Err != nil {
			t.Fatalf("expected no stream error, got %v", event.Err)
		}
		parts = append(parts, event.Chunk.Choices[0].Message.Content)
	}

	if got := strings.Join(parts, ""); got != "hello" {
		t.Fatalf("expected hello, got %s", got)
	}
}

func TestCreateChatCompletionReturnsUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer server.Close()

	p := New(Config{BaseURL: server.URL, DefaultModel: openAITestDefaultModel})

	_, err := p.CreateChatCompletion(context.Background(), provider.ChatCompletionRequest{
		Messages: []provider.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil || !strings.Contains(err.Error(), "bad key") {
		t.Fatalf("expected upstream error, got %v", err)
	}
}

func TestListModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("expected /models, got %s", r.URL.Path)
		}
		w.Header().Set(openAITestContentTypeHeader, openAITestJSONContentType)
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"` + openAITestDefaultModel + `","object":"model","created":123,"owned_by":"openai"}]}`))
	}))
	defer server.Close()

	p := New(Config{BaseURL: server.URL, APIKey: "test-key"})

	models, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(models) != 1 || models[0].ID != openAITestDefaultModel {
		t.Fatalf("unexpected models %#v", models)
	}
}

func TestCreateChatCompletionRetriesRetryableStatus(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":{"message":"temporary failure"}}`))
			return
		}

		w.Header().Set(openAITestContentTypeHeader, openAITestJSONContentType)
		_, _ = w.Write([]byte(openAICompletionResponsePrefix + openAITestDefaultModel + openAICompletionResponseSuffix))
	}))
	defer server.Close()

	p := New(Config{
		BaseURL:      server.URL,
		DefaultModel: openAITestDefaultModel,
		MaxRetries:   1,
		RetryBackoff: time.Millisecond,
	})

	resp, err := p.CreateChatCompletion(context.Background(), provider.ChatCompletionRequest{
		Messages: []provider.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if resp.Choices[0].Message.Content != "hello" {
		t.Fatalf("unexpected response %#v", resp)
	}
}

func TestCreateChatCompletionRetryRecordsAttemptUsage(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":{"message":"temporary failure"}}`))
			return
		}

		w.Header().Set(openAITestContentTypeHeader, openAITestJSONContentType)
		_, _ = w.Write([]byte(openAICompletionResponsePrefix + openAITestDefaultModel + openAICompletionResponseSuffix))
	}))
	defer server.Close()

	recorder := &attemptRecorderStub{}
	ctx := provider.WithAttemptRecorder(context.Background(), recorder)

	p := New(Config{
		BaseURL:      server.URL,
		DefaultModel: openAITestDefaultModel,
		MaxRetries:   1,
		RetryBackoff: time.Millisecond,
	})

	if _, err := p.CreateChatCompletion(ctx, provider.ChatCompletionRequest{
		Messages: []provider.ChatMessage{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("create chat completion: %v", err)
	}

	if len(recorder.records) != 2 {
		t.Fatalf("expected 2 recorded attempts, got %#v", recorder.records)
	}
	if recorder.records[0].metadata.PromptTokens != 2 || recorder.records[0].result.Status != "failed" || recorder.records[0].result.TotalTokens != 2 {
		t.Fatalf("unexpected first attempt record %#v", recorder.records[0])
	}
	if recorder.records[1].result.Status != "succeeded" || recorder.records[1].result.TotalTokens != 4 {
		t.Fatalf("unexpected second attempt record %#v", recorder.records[1])
	}
}

func TestStreamChatCompletionRetriesBeforeOpen(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":{"message":"temporary failure"}}`))
			return
		}

		w.Header().Set(openAITestContentTypeHeader, openAITestEventStreamType)
		_, _ = w.Write([]byte(openAIStreamChunk(openAITestDefaultModel, `{"role":"assistant","content":"ok"}`, "stop")))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p := New(Config{
		BaseURL:      server.URL,
		DefaultModel: openAITestDefaultModel,
		MaxRetries:   1,
		RetryBackoff: time.Millisecond,
	})

	chunks, err := p.StreamChatCompletion(context.Background(), provider.ChatCompletionRequest{
		Messages: []provider.ChatMessage{{Role: "user", Content: "hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}

	var content strings.Builder
	for event := range chunks {
		if event.Err != nil {
			t.Fatalf("expected no stream error, got %v", event.Err)
		}
		content.WriteString(event.Chunk.Choices[0].Message.Content)
	}

	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if content.String() != "ok" {
		t.Fatalf("expected ok, got %s", content.String())
	}
}

func TestCreateChatCompletionHonorsTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set(openAITestContentTypeHeader, openAITestJSONContentType)
		_, _ = w.Write([]byte(openAICompletionResponsePrefix + openAITestDefaultModel + openAICompletionResponseSuffix))
	}))
	defer server.Close()

	p := New(Config{
		BaseURL:      server.URL,
		DefaultModel: openAITestDefaultModel,
		Timeout:      20 * time.Millisecond,
		MaxRetries:   0,
	})

	_, err := p.CreateChatCompletion(context.Background(), provider.ChatCompletionRequest{
		Messages: []provider.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("expected deadline exceeded error, got %v", err)
	}
}

func TestStreamChatCompletionPropagatesMidstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(openAITestContentTypeHeader, openAITestEventStreamType)
		_, _ = w.Write([]byte(openAIStreamChunk(openAITestDefaultModel, `{"role":"assistant","content":"hel"}`, "")))
	}))
	defer server.Close()

	p := New(Config{
		BaseURL:      server.URL,
		DefaultModel: openAITestDefaultModel,
	})

	events, err := p.StreamChatCompletion(context.Background(), provider.ChatCompletionRequest{
		Messages: []provider.ChatMessage{{Role: "user", Content: "hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("stream chat completion: %v", err)
	}

	first, ok := <-events
	if !ok {
		t.Fatal("expected first stream event")
	}
	if first.Err != nil || first.Chunk.Choices[0].Message.Content != "hel" {
		t.Fatalf("unexpected first stream event %#v", first)
	}

	second, ok := <-events
	if !ok {
		t.Fatal("expected terminal error event")
	}
	if second.Err == nil || !strings.Contains(second.Err.Error(), "before [DONE]") {
		t.Fatalf("expected terminal midstream error, got %#v", second)
	}
}

type attemptRecorderStub struct {
	records []*attemptRecord
}

type attemptRecord struct {
	metadata provider.AttemptMetadata
	result   provider.AttemptResult
}

func (r *attemptRecorderStub) BeginAttempt(_ context.Context, metadata provider.AttemptMetadata) (provider.AttemptHandle, error) {
	record := &attemptRecord{metadata: metadata}
	r.records = append(r.records, record)
	return attemptHandleStub{record: record}, nil
}

func openAIStreamChunk(model, deltaJSON, finishReason string) string {
	return fmt.Sprintf(
		"data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":123,\"model\":\"%s\",\"choices\":[{\"index\":0,\"delta\":%s,\"finish_reason\":\"%s\"}]}\n\n",
		model,
		deltaJSON,
		finishReason,
	)
}

type attemptHandleStub struct {
	record *attemptRecord
}

func (h attemptHandleStub) Complete(_ context.Context, result provider.AttemptResult) error {
	if result.TotalTokens == 0 {
		result.TotalTokens = h.record.metadata.PromptTokens + result.CompletionTokens
	}
	if result.PromptTokens == 0 {
		result.PromptTokens = h.record.metadata.PromptTokens
	}
	h.record.result = result
	return nil
}
