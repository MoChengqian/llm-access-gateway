package chat

import (
	"context"
	"errors"
	"testing"

	"github.com/MoChengqian/llm-access-gateway/internal/provider"
)

func TestCreateCompletionRequiresMessages(t *testing.T) {
	providerStub := &stubProvider{}
	service := NewService("gpt-4o-mini", providerStub)

	_, err := service.CreateCompletion(context.Background(), CompletionRequest{})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}

	if providerStub.called {
		t.Fatal("expected provider not to be called when request is invalid")
	}
}

func TestCreateCompletionAppliesDefaultModelAndUsesProvider(t *testing.T) {
	providerStub := &stubProvider{
		response: provider.ChatCompletionResponse{
			ID:      "chatcmpl-mock",
			Object:  "chat.completion",
			Created: 123,
			Model:   "gpt-4o-mini",
			Choices: []provider.ChatChoice{
				{
					Index: 0,
					Message: provider.ChatMessage{
						Role:    "assistant",
						Content: "This is a mock response from LLM Access Gateway.",
					},
					FinishReason: "stop",
				},
			},
			Usage: provider.Usage{},
		},
	}
	service := NewService("gpt-4o-mini", providerStub)

	resp, err := service.CreateCompletion(context.Background(), CompletionRequest{
		Messages: []Message{
			{
				Role:    "user",
				Content: "hello",
			},
		},
	})
	if err != nil {
		t.Fatalf("create completion: %v", err)
	}

	if !providerStub.called {
		t.Fatal("expected provider to be called")
	}

	if providerStub.request.Model != "gpt-4o-mini" {
		t.Fatalf("expected default model to be forwarded, got %s", providerStub.request.Model)
	}

	if resp.Model != "gpt-4o-mini" {
		t.Fatalf("expected response model gpt-4o-mini, got %s", resp.Model)
	}
}

func TestStreamCompletionAppliesDefaultModelAndUsesProvider(t *testing.T) {
	providerStub := &stubProvider{
		streamResponse: []provider.ChatCompletionStreamEvent{
			{
				Chunk: provider.ChatCompletionChunk{
					ID:      "chatcmpl-mock",
					Object:  "chat.completion.chunk",
					Created: 123,
					Model:   "gpt-4o-mini",
					Choices: []provider.ChatChoice{
						{
							Index: 0,
							Message: provider.ChatMessage{
								Role:    "assistant",
								Content: "hello",
							},
							FinishReason: "",
						},
					},
				},
			},
		},
	}
	service := NewService("gpt-4o-mini", providerStub)

	stream, err := service.StreamCompletion(context.Background(), CompletionRequest{
		Stream: true,
		Messages: []Message{
			{
				Role:    "user",
				Content: "hello",
			},
		},
	})
	if err != nil {
		t.Fatalf("stream completion: %v", err)
	}

	var events []CompletionEvent
	for event := range stream {
		events = append(events, event)
	}

	if !providerStub.streamCalled {
		t.Fatal("expected stream provider to be called")
	}

	if !providerStub.request.Stream {
		t.Fatal("expected stream flag to be forwarded to provider")
	}

	if len(events) != 1 || events[0].Chunk.Object != "chat.completion.chunk" {
		t.Fatalf("expected one chat completion chunk event, got %#v", events)
	}

	if got := events[0].Chunk.Choices[0].Delta.Content; got != "hello" {
		t.Fatalf("expected stream chunk delta content hello, got %q", got)
	}

	if got := events[0].Chunk.Choices[0].Delta.Role; got != "assistant" {
		t.Fatalf("expected stream chunk delta role assistant, got %q", got)
	}
}

func TestStreamCompletionStopsOnContextCancellation(t *testing.T) {
	providerStub := &stubProvider{
		streamResponse: []provider.ChatCompletionStreamEvent{
			{
				Chunk: provider.ChatCompletionChunk{
					ID:      "chatcmpl-mock",
					Object:  "chat.completion.chunk",
					Created: 123,
					Model:   "gpt-4o-mini",
					Choices: []provider.ChatChoice{
						{
							Index: 0,
							Message: provider.ChatMessage{
								Role:    "assistant",
								Content: "hello",
							},
						},
					},
				},
			},
		},
	}
	service := NewService("gpt-4o-mini", providerStub)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stream, err := service.StreamCompletion(ctx, CompletionRequest{
		Stream: true,
		Messages: []Message{
			{
				Role:    "user",
				Content: "hello",
			},
		},
	})
	if err != nil {
		t.Fatalf("stream completion: %v", err)
	}

	if event, ok := <-stream; ok {
		t.Fatalf("expected cancelled stream to close without events, got %#v", event)
	}
}

func TestStreamCompletionPropagatesTerminalStreamError(t *testing.T) {
	providerStub := &stubProvider{
		streamResponse: []provider.ChatCompletionStreamEvent{
			{
				Chunk: provider.ChatCompletionChunk{
					ID:      "chatcmpl-mock",
					Object:  "chat.completion.chunk",
					Created: 123,
					Model:   "gpt-4o-mini",
					Choices: []provider.ChatChoice{
						{
							Index: 0,
							Message: provider.ChatMessage{
								Role:    "assistant",
								Content: "hello",
							},
						},
					},
				},
			},
			{Err: errors.New("stream interrupted")},
		},
	}
	service := NewService("gpt-4o-mini", providerStub)

	stream, err := service.StreamCompletion(context.Background(), CompletionRequest{
		Stream: true,
		Messages: []Message{
			{
				Role:    "user",
				Content: "hello",
			},
		},
	})
	if err != nil {
		t.Fatalf("stream completion: %v", err)
	}

	first, ok := <-stream
	if !ok {
		t.Fatal("expected first chunk event")
	}
	if first.Err != nil || first.Chunk.Choices[0].Delta.Content != "hello" {
		t.Fatalf("unexpected first event %#v", first)
	}

	second, ok := <-stream
	if !ok {
		t.Fatal("expected terminal error event")
	}
	if second.Err == nil || second.Err.Error() != "stream interrupted" {
		t.Fatalf("expected terminal stream error, got %#v", second)
	}
}

type stubProvider struct {
	called         bool
	streamCalled   bool
	request        provider.ChatCompletionRequest
	response       provider.ChatCompletionResponse
	streamResponse []provider.ChatCompletionStreamEvent
	err            error
}

func (s *stubProvider) CreateChatCompletion(_ context.Context, req provider.ChatCompletionRequest) (provider.ChatCompletionResponse, error) {
	s.called = true
	s.request = req
	return s.response, s.err
}

func (s *stubProvider) StreamChatCompletion(_ context.Context, req provider.ChatCompletionRequest) (<-chan provider.ChatCompletionStreamEvent, error) {
	s.streamCalled = true
	s.request = req

	events := make(chan provider.ChatCompletionStreamEvent, len(s.streamResponse))
	for _, event := range s.streamResponse {
		events <- event
	}
	close(events)

	return events, s.err
}
