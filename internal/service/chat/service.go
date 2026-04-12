package chat

import (
	"context"
	"errors"
	"strconv"

	"github.com/MoChengqian/llm-access-gateway/internal/obs/tracing"
	"github.com/MoChengqian/llm-access-gateway/internal/provider"
	"go.uber.org/zap"
)

var ErrInvalidRequest = errors.New("messages are required")

type Service interface {
	CreateCompletion(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
	StreamCompletion(ctx context.Context, req CompletionRequest) (<-chan CompletionEvent, error)
}

type service struct {
	defaultModel string
	provider     provider.ChatCompletionProvider
}

type CompletionRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type CompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type CompletionChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
}

type CompletionEvent struct {
	Chunk CompletionChunk
	Err   error
}

type ChunkChoice struct {
	Index        int        `json:"index"`
	Delta        ChunkDelta `json:"delta"`
	FinishReason string     `json:"finish_reason"`
}

type ChunkDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type streamEventResult struct {
	event      CompletionEvent
	chunkSent  bool
	traceErr   error
	shouldStop bool
}

func NewService(defaultModel string, chatProvider provider.ChatCompletionProvider) Service {
	return service{
		defaultModel: defaultModel,
		provider:     chatProvider,
	}
}

func (s service) CreateCompletion(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	ctx, span := tracing.StartSpan(ctx, "chat.service.create_completion",
		zap.String("model", req.Model),
		zap.String("stream", strconv.FormatBool(false)),
	)
	var traceErr error
	defer func() {
		span.End(traceErr)
	}()

	providerReq, err := s.prepareProviderRequest(req)
	if err != nil {
		traceErr = err
		return CompletionResponse{}, err
	}

	providerResp, err := s.provider.CreateChatCompletion(ctx, providerReq)
	if err != nil {
		traceErr = err
		return CompletionResponse{}, err
	}

	return CompletionResponse{
		ID:      providerResp.ID,
		Object:  providerResp.Object,
		Created: providerResp.Created,
		Model:   providerResp.Model,
		Choices: toChoices(providerResp.Choices),
		Usage: Usage{
			PromptTokens:     providerResp.Usage.PromptTokens,
			CompletionTokens: providerResp.Usage.CompletionTokens,
			TotalTokens:      providerResp.Usage.TotalTokens,
		},
	}, nil
}

func (s service) StreamCompletion(ctx context.Context, req CompletionRequest) (<-chan CompletionEvent, error) {
	ctx, span := tracing.StartSpan(ctx, "chat.service.stream_completion",
		zap.String("model", req.Model),
		zap.String("stream", strconv.FormatBool(true)),
	)

	providerReq, err := s.prepareProviderRequest(req)
	if err != nil {
		span.End(err)
		return nil, err
	}

	providerChunks, err := s.provider.StreamChatCompletion(ctx, providerReq)
	if err != nil {
		span.End(err)
		return nil, err
	}

	serviceEvents := make(chan CompletionEvent)
	go func() {
		var traceErr error
		chunkCount := 0
		defer func() {
			span.End(traceErr, zap.Int("chunk_count", chunkCount))
		}()
		defer close(serviceEvents)
		for event := range providerChunks {
			result := handleProviderStreamEvent(ctx, event, serviceEvents)
			if result.chunkSent {
				chunkCount++
			}
			if result.traceErr != nil {
				traceErr = result.traceErr
			}
			if result.shouldStop {
				return
			}
		}
	}()

	return serviceEvents, nil
}

func handleProviderStreamEvent(ctx context.Context, event provider.ChatCompletionStreamEvent, serviceEvents chan<- CompletionEvent) streamEventResult {
	if err := ctx.Err(); err != nil {
		return streamEventResult{
			traceErr:   err,
			shouldStop: true,
		}
	}

	if event.Err != nil {
		if err := ctx.Err(); err != nil {
			return streamEventResult{
				traceErr:   err,
				shouldStop: true,
			}
		}

		serviceEvents <- CompletionEvent{Err: event.Err}
		return streamEventResult{
			traceErr:   event.Err,
			shouldStop: true,
		}
	}

	if err := ctx.Err(); err != nil {
		return streamEventResult{
			traceErr:   err,
			shouldStop: true,
		}
	}

	serviceEvents <- CompletionEvent{
		Chunk: toCompletionChunk(event.Chunk),
	}
	return streamEventResult{
		chunkSent: true,
	}
}

func (s service) prepareProviderRequest(req CompletionRequest) (provider.ChatCompletionRequest, error) {
	if len(req.Messages) == 0 {
		return provider.ChatCompletionRequest{}, ErrInvalidRequest
	}

	model := req.Model
	if model == "" {
		model = s.defaultModel
	}

	return provider.ChatCompletionRequest{
		Model:    model,
		Messages: toProviderMessages(req.Messages),
		Stream:   req.Stream,
	}, nil
}

func toProviderMessages(messages []Message) []provider.ChatMessage {
	providerMessages := make([]provider.ChatMessage, 0, len(messages))
	for _, message := range messages {
		providerMessages = append(providerMessages, provider.ChatMessage{
			Role:    message.Role,
			Content: message.Content,
		})
	}

	return providerMessages
}

func toChoices(choices []provider.ChatChoice) []Choice {
	serviceChoices := make([]Choice, 0, len(choices))
	for _, choice := range choices {
		serviceChoices = append(serviceChoices, Choice{
			Index: choice.Index,
			Message: Message{
				Role:    choice.Message.Role,
				Content: choice.Message.Content,
			},
			FinishReason: choice.FinishReason,
		})
	}

	return serviceChoices
}

func toChunkChoices(choices []provider.ChatChoice) []ChunkChoice {
	serviceChoices := make([]ChunkChoice, 0, len(choices))
	for _, choice := range choices {
		serviceChoice := ChunkChoice{
			Index:        choice.Index,
			FinishReason: choice.FinishReason,
		}

		if choice.Message.Role != "" {
			serviceChoice.Delta.Role = choice.Message.Role
		}

		if choice.Message.Content != "" {
			serviceChoice.Delta.Content = choice.Message.Content
		}

		serviceChoices = append(serviceChoices, serviceChoice)
	}

	return serviceChoices
}

func toCompletionChunk(chunk provider.ChatCompletionChunk) CompletionChunk {
	return CompletionChunk{
		ID:      chunk.ID,
		Object:  chunk.Object,
		Created: chunk.Created,
		Model:   chunk.Model,
		Choices: toChunkChoices(chunk.Choices),
	}
}
