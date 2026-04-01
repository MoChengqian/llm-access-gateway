package chat

import (
	"context"
	"errors"
	"time"
)

var ErrInvalidRequest = errors.New("messages are required")

type Service interface {
	CreateCompletion(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

type MockService struct {
	defaultModel string
}

type CompletionRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
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

func NewMockService(defaultModel string) MockService {
	return MockService{defaultModel: defaultModel}
}

func (s MockService) CreateCompletion(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
	if len(req.Messages) == 0 {
		return CompletionResponse{}, ErrInvalidRequest
	}

	model := req.Model
	if model == "" {
		model = s.defaultModel
	}

	return CompletionResponse{
		ID:      "chatcmpl-mock",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []Choice{
			{
				Index: 0,
				Message: Message{
					Role:    "assistant",
					Content: "This is a mock response from LLM Access Gateway.",
				},
				FinishReason: "stop",
			},
		},
		Usage: Usage{
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
		},
	}, nil
}
