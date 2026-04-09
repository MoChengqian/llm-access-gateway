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
		streamChunks: []provider.ChatCompletionStreamEvent{{Chunk: provider.ChatCompletionChunk{Model: "secondary"}}},
	}

	routed := New([]Backend{
		{Name: "primary", Provider: primary},
		{Name: "secondary", Provider: secondary},
	}, Config{FailureThreshold: 1, Cooldown: time.Minute})

	chunks, err := routed.StreamChatCompletion(context.Background(), provider.ChatCompletionRequest{Model: "gpt-4o-mini", Stream: true})
	if err != nil {
		t.Fatalf("stream completion: %v", err)
	}

	event := <-chunks
	if event.Chunk.Model != "secondary" {
		t.Fatalf("expected secondary stream chunk, got %#v", event)
	}
}

func TestStreamCompletionFallsBackWhenPrimaryErrorsBeforeFirstChunk(t *testing.T) {
	primary := &stubProvider{
		streamChunks: []provider.ChatCompletionStreamEvent{
			{Err: errors.New("primary interrupted before first chunk")},
		},
	}
	secondary := &stubProvider{
		streamChunks: []provider.ChatCompletionStreamEvent{
			{Chunk: provider.ChatCompletionChunk{Model: "secondary"}},
		},
	}

	routed := New([]Backend{
		{Name: "primary", Provider: primary},
		{Name: "secondary", Provider: secondary},
	}, Config{FailureThreshold: 1, Cooldown: time.Minute})

	events, err := routed.StreamChatCompletion(context.Background(), provider.ChatCompletionRequest{Model: "gpt-4o-mini", Stream: true})
	if err != nil {
		t.Fatalf("stream completion: %v", err)
	}

	first := <-events
	if first.Err != nil || first.Chunk.Model != "secondary" {
		t.Fatalf("expected fallback chunk from secondary, got %#v", first)
	}
}

func TestStreamCompletionDoesNotFallbackAfterFirstChunk(t *testing.T) {
	primary := &stubProvider{
		streamChunks: []provider.ChatCompletionStreamEvent{
			{Chunk: provider.ChatCompletionChunk{Model: "primary"}},
			{Err: errors.New("primary interrupted after first chunk")},
		},
	}
	secondary := &stubProvider{
		streamChunks: []provider.ChatCompletionStreamEvent{
			{Chunk: provider.ChatCompletionChunk{Model: "secondary"}},
		},
	}

	routed := New([]Backend{
		{Name: "primary", Provider: primary},
		{Name: "secondary", Provider: secondary},
	}, Config{FailureThreshold: 1, Cooldown: time.Minute})

	events, err := routed.StreamChatCompletion(context.Background(), provider.ChatCompletionRequest{Model: "gpt-4o-mini", Stream: true})
	if err != nil {
		t.Fatalf("stream completion: %v", err)
	}

	first := <-events
	if first.Err != nil || first.Chunk.Model != "primary" {
		t.Fatalf("expected first chunk from primary, got %#v", first)
	}

	second := <-events
	if second.Err == nil || second.Err.Error() != "primary interrupted after first chunk" {
		t.Fatalf("expected terminal interruption event, got %#v", second)
	}
	if secondary.streamCalled {
		t.Fatal("expected secondary not to be used after first chunk")
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

func TestCreateCompletionPrefersModelMatchedHigherPriorityBackend(t *testing.T) {
	generic := &stubProvider{
		response: provider.ChatCompletionResponse{Model: "generic"},
	}
	matched := &stubProvider{
		response: provider.ChatCompletionResponse{Model: "matched"},
	}
	nonMatching := &stubProvider{
		response: provider.ChatCompletionResponse{Model: "other"},
	}

	routed := New([]Backend{
		{Name: "generic", Priority: 200, Provider: generic},
		{Name: "other", Priority: 10, Models: []string{"claude-3-7-sonnet"}, Provider: nonMatching},
		{Name: "matched", Priority: 50, Models: []string{"gpt-4o-mini"}, Provider: matched},
	}, Config{FailureThreshold: 1, Cooldown: time.Minute})

	resp, err := routed.CreateChatCompletion(context.Background(), provider.ChatCompletionRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatalf("create completion: %v", err)
	}

	if resp.Model != "matched" {
		t.Fatalf("expected matched response first, got %#v", resp)
	}
	if !matched.createCalled {
		t.Fatal("expected matched backend to be attempted")
	}
	if generic.createCalled {
		t.Fatal("expected generic backend not to be used after matched success")
	}
	if nonMatching.createCalled {
		t.Fatal("expected non-matching backend to stay behind generic fallback")
	}
}

func TestCreateCompletionFallsBackFromMatchedBackendToGenericBackend(t *testing.T) {
	matched := &stubProvider{createErr: errors.New("matched failed")}
	generic := &stubProvider{
		response: provider.ChatCompletionResponse{Model: "generic"},
	}
	nonMatching := &stubProvider{
		response: provider.ChatCompletionResponse{Model: "other"},
	}

	routed := New([]Backend{
		{Name: "non-matching", Priority: 1, Models: []string{"claude-3-7-sonnet"}, Provider: nonMatching},
		{Name: "generic", Priority: 100, Provider: generic},
		{Name: "matched", Priority: 50, Models: []string{"gpt-4o-mini"}, Provider: matched},
	}, Config{FailureThreshold: 1, Cooldown: time.Minute})

	resp, err := routed.CreateChatCompletion(context.Background(), provider.ChatCompletionRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatalf("create completion: %v", err)
	}

	if resp.Model != "generic" {
		t.Fatalf("expected generic fallback response, got %#v", resp)
	}
	if !matched.createCalled || !generic.createCalled {
		t.Fatalf("expected matched and generic backends to be attempted, got matched=%v generic=%v", matched.createCalled, generic.createCalled)
	}
	if nonMatching.createCalled {
		t.Fatal("expected non-matching backend to remain last resort")
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

func TestObserverSeesFallbackAndFailureEvents(t *testing.T) {
	observer := &stubObserver{}
	primary := &stubProvider{createErr: errors.New("primary failed")}
	secondary := &stubProvider{
		response: provider.ChatCompletionResponse{Model: "secondary"},
	}

	routed := New([]Backend{
		{Name: "primary", Provider: primary},
		{Name: "secondary", Provider: secondary},
	}, Config{
		FailureThreshold: 1,
		Cooldown:         time.Minute,
		Observer:         observer,
	})

	if _, err := routed.CreateChatCompletion(context.Background(), provider.ChatCompletionRequest{Model: "gpt-4o-mini"}); err != nil {
		t.Fatalf("create completion: %v", err)
	}

	if !observer.contains("provider_request_failed") {
		t.Fatalf("expected provider_request_failed event, got %#v", observer.events)
	}
	if !observer.contains("provider_fallback_succeeded") {
		t.Fatalf("expected provider_fallback_succeeded event, got %#v", observer.events)
	}
}

func TestObserverSeesSkippedBackendDuringCooldown(t *testing.T) {
	now := time.Unix(123, 0)
	observer := &stubObserver{}
	primary := &stubProvider{createErr: errors.New("primary failed")}
	secondary := &stubProvider{
		response: provider.ChatCompletionResponse{Model: "secondary"},
	}

	routed := New([]Backend{
		{Name: "primary", Provider: primary},
		{Name: "secondary", Provider: secondary},
	}, Config{
		FailureThreshold: 1,
		Cooldown:         time.Minute,
		Observer:         observer,
	})
	routed.now = func() time.Time { return now }

	if _, err := routed.CreateChatCompletion(context.Background(), provider.ChatCompletionRequest{Model: "gpt-4o-mini"}); err != nil {
		t.Fatalf("first create completion: %v", err)
	}

	observer.events = nil
	primary.createCalled = false
	secondary.createCalled = false

	if _, err := routed.CreateChatCompletion(context.Background(), provider.ChatCompletionRequest{Model: "gpt-4o-mini"}); err != nil {
		t.Fatalf("second create completion: %v", err)
	}

	if !observer.contains("provider_skipped_unhealthy") {
		t.Fatalf("expected provider_skipped_unhealthy event, got %#v", observer.events)
	}
}

func TestProbeMarksBackendHealthyAndUnhealthy(t *testing.T) {
	now := time.Unix(123, 0)
	primary := &stubProvider{modelsErr: errors.New("probe failed")}
	secondary := &stubProvider{models: []provider.Model{{ID: "gpt-4o-mini"}}}

	routed := New([]Backend{
		{Name: "primary", Provider: primary},
		{Name: "secondary", Provider: secondary},
	}, Config{FailureThreshold: 1, Cooldown: time.Minute})
	routed.now = func() time.Time { return now }

	routed.Probe(context.Background())

	statuses := routed.BackendStatuses()
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %#v", statuses)
	}

	if statuses[0].Healthy {
		t.Fatalf("expected primary unhealthy after failed probe, got %#v", statuses[0])
	}
	if statuses[0].LastProbeError == "" || statuses[0].LastProbeAt.IsZero() {
		t.Fatalf("expected probe metadata, got %#v", statuses[0])
	}
	if !statuses[1].Healthy || statuses[1].LastProbeAt.IsZero() {
		t.Fatalf("expected secondary healthy after successful probe, got %#v", statuses[1])
	}
}

func TestProbeObserverSeesProbeEvents(t *testing.T) {
	observer := &stubObserver{}
	primary := &stubProvider{modelsErr: errors.New("probe failed")}
	secondary := &stubProvider{models: []provider.Model{{ID: "gpt-4o-mini"}}}

	routed := New([]Backend{
		{Name: "primary", Provider: primary},
		{Name: "secondary", Provider: secondary},
	}, Config{
		FailureThreshold: 1,
		Cooldown:         time.Minute,
		Observer:         observer,
	})

	routed.Probe(context.Background())

	if !observer.contains("provider_probe_failed") {
		t.Fatalf("expected provider_probe_failed event, got %#v", observer.events)
	}
	if !observer.contains("provider_probe_succeeded") {
		t.Fatalf("expected provider_probe_succeeded event, got %#v", observer.events)
	}
}

type stubProvider struct {
	createCalled bool
	streamCalled bool
	modelsCalled bool
	createErr    error
	streamErr    error
	modelsErr    error
	response     provider.ChatCompletionResponse
	streamChunks []provider.ChatCompletionStreamEvent
	models       []provider.Model
}

func (s *stubProvider) CreateChatCompletion(context.Context, provider.ChatCompletionRequest) (provider.ChatCompletionResponse, error) {
	s.createCalled = true
	if s.createErr != nil {
		return provider.ChatCompletionResponse{}, s.createErr
	}
	return s.response, nil
}

func (s *stubProvider) StreamChatCompletion(context.Context, provider.ChatCompletionRequest) (<-chan provider.ChatCompletionStreamEvent, error) {
	s.streamCalled = true
	if s.streamErr != nil {
		return nil, s.streamErr
	}

	events := make(chan provider.ChatCompletionStreamEvent, len(s.streamChunks))
	for _, event := range s.streamChunks {
		events <- event
	}
	close(events)
	return events, nil
}

func (s *stubProvider) ListModels(context.Context) ([]provider.Model, error) {
	s.modelsCalled = true
	if s.modelsErr != nil {
		return nil, s.modelsErr
	}
	return s.models, nil
}

type stubObserver struct {
	events []Event
}

func (o *stubObserver) OnEvent(event Event) {
	o.events = append(o.events, event)
}

func (o *stubObserver) contains(eventType string) bool {
	for _, event := range o.events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}
