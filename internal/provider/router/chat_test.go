package router

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/provider"
)

func TestCreateCompletionFallsBackToSecondary(t *testing.T) {
	primary := &stubProvider{createErr: errors.New("primary failed")}
	secondary := &stubProvider{
		response: provider.ChatCompletionResponse{Model: "secondary"},
	}

	routed := New([]Backend{
		{Name: "primary", Provider: primary},
		{Name: "secondary", Provider: secondary},
	}, Config{FailureThreshold: 1, Cooldown: time.Minute})

	resp, err := routed.CreateChatCompletion(context.Background(), provider.ChatCompletionRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatalf("create completion: %v", err)
	}

	if !primary.createCalled || !secondary.createCalled {
		t.Fatalf("expected both providers to be attempted, got primary=%v secondary=%v", primary.createCalled, secondary.createCalled)
	}

	if resp.Model != "secondary" {
		t.Fatalf("expected secondary response, got %#v", resp)
	}
}

func TestStreamCompletionFallsBackBeforeFirstChunk(t *testing.T) {
	primary := &stubProvider{streamErr: errors.New("primary stream failed")}
	secondary := &stubProvider{
		streamChunks: []provider.ChatCompletionChunk{{Model: "secondary"}},
	}

	routed := New([]Backend{
		{Name: "primary", Provider: primary},
		{Name: "secondary", Provider: secondary},
	}, Config{FailureThreshold: 1, Cooldown: time.Minute})

	chunks, err := routed.StreamChatCompletion(context.Background(), provider.ChatCompletionRequest{Model: "gpt-4o-mini", Stream: true})
	if err != nil {
		t.Fatalf("stream completion: %v", err)
	}

	chunk := <-chunks
	if chunk.Model != "secondary" {
		t.Fatalf("expected secondary stream chunk, got %#v", chunk)
	}
}

func TestUnhealthyPrimaryIsSkippedDuringCooldown(t *testing.T) {
	now := time.Unix(123, 0)
	primary := &stubProvider{createErr: errors.New("primary failed")}
	secondary := &stubProvider{
		response: provider.ChatCompletionResponse{Model: "secondary"},
	}

	routed := New([]Backend{
		{Name: "primary", Provider: primary},
		{Name: "secondary", Provider: secondary},
	}, Config{FailureThreshold: 1, Cooldown: time.Minute})
	routed.now = func() time.Time { return now }

	if _, err := routed.CreateChatCompletion(context.Background(), provider.ChatCompletionRequest{Model: "gpt-4o-mini"}); err != nil {
		t.Fatalf("first create completion: %v", err)
	}

	primary.createCalled = false
	secondary.createCalled = false

	if _, err := routed.CreateChatCompletion(context.Background(), provider.ChatCompletionRequest{Model: "gpt-4o-mini"}); err != nil {
		t.Fatalf("second create completion: %v", err)
	}

	if primary.createCalled {
		t.Fatal("expected unhealthy primary to be skipped during cooldown")
	}

	if !secondary.createCalled {
		t.Fatal("expected secondary to be used during cooldown")
	}
}

func TestReadyAndBackendStatusesReflectCooldown(t *testing.T) {
	now := time.Unix(123, 0)
	primary := &stubProvider{createErr: errors.New("primary failed")}
	secondary := &stubProvider{createErr: errors.New("secondary failed")}

	routed := New([]Backend{
		{Name: "primary", Provider: primary},
		{Name: "secondary", Provider: secondary},
	}, Config{FailureThreshold: 1, Cooldown: time.Minute})
	routed.now = func() time.Time { return now }

	if _, err := routed.CreateChatCompletion(context.Background(), provider.ChatCompletionRequest{Model: "gpt-4o-mini"}); err == nil {
		t.Fatal("expected create completion to fail")
	}

	if routed.Ready() {
		t.Fatal("expected router to be unready while all backends are in cooldown")
	}

	statuses := routed.BackendStatuses()
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %#v", statuses)
	}

	for _, status := range statuses {
		if status.Healthy {
			t.Fatalf("expected backend to be unhealthy, got %#v", statuses)
		}
		if status.ConsecutiveFailures != 1 {
			t.Fatalf("expected one failure recorded, got %#v", statuses)
		}
	}
}

type stubProvider struct {
	createCalled bool
	streamCalled bool
	createErr    error
	streamErr    error
	response     provider.ChatCompletionResponse
	streamChunks []provider.ChatCompletionChunk
}

func (s *stubProvider) CreateChatCompletion(context.Context, provider.ChatCompletionRequest) (provider.ChatCompletionResponse, error) {
	s.createCalled = true
	if s.createErr != nil {
		return provider.ChatCompletionResponse{}, s.createErr
	}
	return s.response, nil
}

func (s *stubProvider) StreamChatCompletion(context.Context, provider.ChatCompletionRequest) (<-chan provider.ChatCompletionChunk, error) {
	s.streamCalled = true
	if s.streamErr != nil {
		return nil, s.streamErr
	}

	chunks := make(chan provider.ChatCompletionChunk, len(s.streamChunks))
	for _, chunk := range s.streamChunks {
		chunks <- chunk
	}
	close(chunks)
	return chunks, nil
}
