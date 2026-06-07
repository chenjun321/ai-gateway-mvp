package proxy

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrDownstream = errors.New("downstream provider error")

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	ThreadID string        `json:"thread_id,omitempty"`
	Stream   bool          `json:"stream,omitempty"`
}

type ChatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
	Usage   Usage        `json:"usage"`
}

type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type Provider interface {
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
}

type MockProvider struct{}

func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

func (p *MockProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	switch req.Model {
	case "mock-error":
		return nil, ErrDownstream
	case "mock-timeout":
		select {
		case <-time.After(10 * time.Second):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	lastUser := "hello"
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" && strings.TrimSpace(req.Messages[i].Content) != "" {
			lastUser = req.Messages[i].Content
			break
		}
	}
	content := "Mock response: " + lastUser
	promptTokens := CountMessages(req.Messages)
	completionTokens := CountText(content)
	return &ChatResponse{
		ID:      "chatcmpl-mock",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []ChatChoice{
			{
				Index: 0,
				Message: ChatMessage{
					Role:    "assistant",
					Content: content,
				},
				FinishReason: "stop",
			},
		},
		Usage: Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}, nil
}

func CountMessages(messages []ChatMessage) int {
	total := 0
	for _, msg := range messages {
		total += CountText(msg.Role) + CountText(msg.Content) + 4
	}
	if total == 0 {
		return 0
	}
	return total + 2
}

func CountText(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	words := len(strings.Fields(trimmed))
	chars := len([]rune(trimmed))
	byChars := (chars + 3) / 4
	if words > byChars {
		return words
	}
	return byChars
}
