package provider

import "strings"

func EstimatePromptTokens(messages []ChatMessage) int {
	var builder strings.Builder
	for _, message := range messages {
		builder.WriteString(message.Role)
		builder.WriteString(" ")
		builder.WriteString(message.Content)
		builder.WriteString(" ")
	}

	return EstimateTextTokens(builder.String())
}

func EstimateTextTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}

	fields := strings.Fields(text)
	if len(fields) > 0 {
		return len(fields)
	}

	return len([]rune(text))
}
