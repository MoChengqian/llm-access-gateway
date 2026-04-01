package mock

import (
	"context"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/provider"
)

type Provider struct{}

func New() Provider {
	return Provider{}
}

func (Provider) CreateChatCompletion(_ context.Context, req provider.ChatCompletionRequest) (provider.ChatCompletionResponse, error) {
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
					Content: "This is a mock response from LLM Access Gateway.",
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

func (Provider) StreamChatCompletion(_ context.Context, req provider.ChatCompletionRequest) (<-chan provider.ChatCompletionChunk, error) {
	chunks := make(chan provider.ChatCompletionChunk, 4)

	go func() {
		defer close(chunks)

		now := time.Now().Unix()
		id := "chatcmpl-mock"
		parts := []string{
			"This is ",
			"a mock response ",
			"from LLM Access Gateway.",
		}

		for index, part := range parts {
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
