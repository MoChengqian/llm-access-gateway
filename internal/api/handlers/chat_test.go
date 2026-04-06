package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/auth"
	"github.com/MoChengqian/llm-access-gateway/internal/service/chat"
	"github.com/MoChengqian/llm-access-gateway/internal/service/governance"
)

func TestCreateCompletionReturnsJSONErrorWhenStreamFailsBeforeFirstChunk(t *testing.T) {
	store := &stubGovernanceStore{insertID: 1}
	handler := NewChatHandler(
		stubChatService{
			streamEvents: []chat.CompletionEvent{
				{Err: errors.New("upstream interrupted before first chunk")},
			},
		},
		governance.NewService(store, stubLimiter{}),
		nil,
	)

	req := newStreamRequest(t)
	rec := httptest.NewRecorder()

	handler.CreateCompletion(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"error":"internal server error"`) {
		t.Fatalf("expected internal server error body, got %s", body)
	}
	if strings.Contains(rec.Body.String(), "data: [DONE]") {
		t.Fatalf("expected no DONE marker on failed stream, got %s", rec.Body.String())
	}
	if store.updated.Status != "failed" {
		t.Fatalf("expected usage status failed, got %#v", store.updated)
	}
}

func TestCreateCompletionDoesNotWriteDoneWhenStreamFailsAfterFirstChunk(t *testing.T) {
	store := &stubGovernanceStore{insertID: 1}
	handler := NewChatHandler(
		stubChatService{
			streamEvents: []chat.CompletionEvent{
				{
					Chunk: chat.CompletionChunk{
						ID:      "chatcmpl-1",
						Object:  "chat.completion.chunk",
						Created: 123,
						Model:   "gpt-4o-mini",
						Choices: []chat.ChunkChoice{
							{
								Index: 0,
								Delta: chat.ChunkDelta{
									Role:    "assistant",
									Content: "hello",
								},
							},
						},
					},
				},
				{Err: errors.New("upstream interrupted after first chunk")},
			},
		},
		governance.NewService(store, stubLimiter{}),
		nil,
	)

	req := newStreamRequest(t)
	rec := httptest.NewRecorder()

	handler.CreateCompletion(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("expected text/event-stream content type, got %q", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"object":"chat.completion.chunk"`) {
		t.Fatalf("expected chunk payload in body, got %s", body)
	}
	if strings.Contains(body, "data: [DONE]") {
		t.Fatalf("expected no DONE marker on interrupted stream, got %s", body)
	}
	if store.updated.Status != "failed" {
		t.Fatalf("expected usage status failed, got %#v", store.updated)
	}
}

func newStreamRequest(t *testing.T) *http.Request {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": "hello",
			},
		},
		"stream": true,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{
		Tenant: auth.Tenant{
			ID:   1,
			Name: "acme",
		},
		APIKeyID: 1,
	}))
}

type stubChatService struct {
	streamEvents []chat.CompletionEvent
	streamErr    error
}

func (s stubChatService) CreateCompletion(context.Context, chat.CompletionRequest) (chat.CompletionResponse, error) {
	return chat.CompletionResponse{}, nil
}

func (s stubChatService) StreamCompletion(context.Context, chat.CompletionRequest) (<-chan chat.CompletionEvent, error) {
	events := make(chan chat.CompletionEvent, len(s.streamEvents))
	for _, event := range s.streamEvents {
		events <- event
	}
	close(events)
	return events, s.streamErr
}

type stubGovernanceStore struct {
	insertID uint64
	updated  governance.UsageUpdate
}

func (s *stubGovernanceStore) SumTotalTokens(context.Context, uint64) (int, error) {
	return 0, nil
}

func (s *stubGovernanceStore) InsertUsageRecord(context.Context, governance.UsageRecord) (uint64, error) {
	return s.insertID, nil
}

func (s *stubGovernanceStore) UpdateUsageRecord(_ context.Context, update governance.UsageUpdate) error {
	s.updated = update
	return nil
}

type stubLimiter struct{}

func (stubLimiter) Admit(context.Context, auth.Principal, int, time.Time) error {
	return nil
}

func (stubLimiter) RecordCompletionTokens(context.Context, auth.Principal, int, time.Time) error {
	return nil
}
