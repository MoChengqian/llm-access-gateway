package chat

import (
	"context"
	"errors"

	"github.com/MoChengqian/llm-access-gateway/internal/provider"
)

var ErrInvalidRequest = errors.New("messages are required")

type Service interface {
	CreateCompletion(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
	StreamCompletion(ctx context.Context, req CompletionRequest) (<-chan CompletionChunk, error)
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

type ChunkChoice struct {
	Index        int        `json:"index"`
	Delta        ChunkDelta `json:"delta"`
	FinishReason string     `json:"finish_reason"`
}

type ChunkDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

func NewService(defaultModel string, chatProvider provider.ChatCompletionProvider) Service {
	return service{
		defaultModel: defaultModel,
		provider:     chatProvider,
	}
}

func (s service) CreateCompletion(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	providerReq, err := s.prepareProviderRequest(req)
	if err != nil {
		return CompletionResponse{}, err
	}

	providerResp, err := s.provider.CreateChatCompletion(ctx, providerReq)
	if err != nil {
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

func (s service) StreamCompletion(ctx context.Context, req CompletionRequest) (<-chan CompletionChunk, error) {
	providerReq, err := s.prepareProviderRequest(req)
	if err != nil {
		return nil, err
	}

	providerChunks, err := s.provider.StreamChatCompletion(ctx, providerReq)
	if err != nil {
		return nil, err
	}

	serviceChunks := make(chan CompletionChunk)
	go func() {
		defer close(serviceChunks)
		for chunk := range providerChunks {
			select {
			case <-ctx.Done():
				return
			case serviceChunks <- CompletionChunk{
				ID:      chunk.ID,
				Object:  chunk.Object,
				Created: chunk.Created,
				Model:   chunk.Model,
				Choices: toChunkChoices(chunk.Choices),
			}:
			}
		}
	}()

	return serviceChunks, nil
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
