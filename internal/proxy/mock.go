package proxy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"ai-gateway-mvp/internal/config"
)

var ErrDownstream = errors.New("downstream provider error")
var ErrUnsupportedModel = errors.New("unsupported model")

const (
	ModelBehaviorSuccess = "success"
	ModelBehaviorError   = "error"
	ModelBehaviorTimeout = "timeout"
)

type ModelConfig = config.ModelConfig

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
	Models() []string
	Supports(model string) bool
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
}

type MockProvider struct {
	models     map[string]ModelConfig
	modelNames []string
}

func NewMockProvider(models []ModelConfig) (*MockProvider, error) {
	if len(models) == 0 {
		return nil, errors.New("mock provider requires at least one model")
	}
	byName := make(map[string]ModelConfig, len(models))
	modelNames := make([]string, 0, len(models))
	for _, model := range models {
		model.Name = strings.TrimSpace(model.Name)
		model.Behavior = strings.TrimSpace(model.Behavior)
		if model.Name == "" {
			return nil, errors.New("model name is required")
		}
		if model.Behavior == "" {
			model.Behavior = ModelBehaviorSuccess
		}
		switch model.Behavior {
		case ModelBehaviorSuccess, ModelBehaviorError, ModelBehaviorTimeout:
		default:
			return nil, fmt.Errorf("model %q has unsupported behavior %q", model.Name, model.Behavior)
		}
		if _, exists := byName[model.Name]; exists {
			return nil, fmt.Errorf("duplicate model %q", model.Name)
		}
		byName[model.Name] = model
		modelNames = append(modelNames, model.Name)
	}
	return &MockProvider{models: byName, modelNames: modelNames}, nil
}

func (p *MockProvider) Models() []string {
	models := make([]string, len(p.modelNames))
	copy(models, p.modelNames)
	return models
}

func (p *MockProvider) Supports(model string) bool {
	_, ok := p.models[model]
	return ok
}

func (p *MockProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	model, ok := p.models[req.Model]
	if !ok {
		return nil, ErrUnsupportedModel
	}
	switch model.Behavior {
	case ModelBehaviorError:
		return nil, ErrDownstream
	case ModelBehaviorTimeout:
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
