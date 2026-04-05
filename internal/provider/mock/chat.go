package mock

import (
	"context"
	"errors"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/provider"
)

var (
	ErrCreateFailed = errors.New("mock provider create failed")
	ErrStreamFailed = errors.New("mock provider stream failed")
)

type Config struct {
	ResponseText string
	StreamParts  []string
	FailCreate   bool
	FailStream   bool
}

type Provider struct {
	responseText string
	streamParts  []string
	failCreate   bool
	failStream   bool
}

func New() Provider {
	return NewWithConfig(Config{})
}

func NewWithConfig(cfg Config) Provider {
	responseText := cfg.ResponseText
	if responseText == "" {
		responseText = "This is a mock response from LLM Access Gateway."
	}

	streamParts := cfg.StreamParts
	if len(streamParts) == 0 {
		streamParts = []string{
			"This is ",
			"a mock response ",
			"from LLM Access Gateway.",
		}
	}

	return Provider{
		responseText: responseText,
		streamParts:  streamParts,
		failCreate:   cfg.FailCreate,
		failStream:   cfg.FailStream,
	}
}

func (p Provider) CreateChatCompletion(_ context.Context, req provider.ChatCompletionRequest) (provider.ChatCompletionResponse, error) {
	if p.failCreate {
		return provider.ChatCompletionResponse{}, ErrCreateFailed
	}

	return provider.ChatCompletionResponse{
		ID:      "chatcmpl-mock",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []provider.ChatChoice{
			{
				Index: 0,
				Message: provider.ChatMessage{
					Role:    "assistant",
					Content: p.responseText,
				},
				FinishReason: "stop",
			},
		},
		Usage: provider.Usage{
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
		},
	}, nil
}

func (p Provider) StreamChatCompletion(_ context.Context, req provider.ChatCompletionRequest) (<-chan provider.ChatCompletionChunk, error) {
	if p.failStream {
		return nil, ErrStreamFailed
	}

	chunks := make(chan provider.ChatCompletionChunk, 4)

	go func() {
		defer close(chunks)

		now := time.Now().Unix()
		id := "chatcmpl-mock"

		for index, part := range p.streamParts {
			chunks <- provider.ChatCompletionChunk{
				ID:      id,
				Object:  "chat.completion.chunk",
				Created: now,
				Model:   req.Model,
				Choices: []provider.ChatChoice{
					{
						Index: index,
						Message: provider.ChatMessage{
							Role:    "assistant",
							Content: part,
						},
						FinishReason: "",
					},
				},
			}
		}

		chunks <- provider.ChatCompletionChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: now,
			Model:   req.Model,
			Choices: []provider.ChatChoice{
				{
					Index: 0,
					Message: provider.ChatMessage{
						Role:    "assistant",
						Content: "",
					},
					FinishReason: "stop",
				},
			},
		}
	}()

	return chunks, nil
}
