package proxy

import (
	"context"
	"errors"
	"testing"

	"ai-gateway-mvp/internal/config"
)

func testConfig() *config.Config {
	return &config.Config{
		Scopes: config.ScopeConfig{
			ChatInvoke:  "chat:invoke",
			ModelPrefix: "model:",
		},
		Chat: config.ChatConfig{
			AllowedRoles:  []string{"system", "user", "assistant", "tool"},
			UserRole:      "user",
			AssistantRole: "assistant",
		},
		Mock: config.MockConfig{
			ResponseID:          "chatcmpl-test",
			ResponseObject:      "chat.completion",
			ResponsePrefix:      "Configured response: ",
			FallbackUserContent: "fallback",
			FinishReason:        "done",
		},
		UsageEstimation: config.UsageEstimationConfig{
			CharsPerToken:         4,
			MessageOverheadTokens: 4,
			PromptOverheadTokens:  2,
		},
		Models: []config.ModelConfig{{Name: "mock-gpt", Behavior: ModelBehaviorSuccess}},
	}
}

func TestMockProviderRejectsUnconfiguredModel(t *testing.T) {
	provider, err := NewMockProvider(testConfig())
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	if provider.Supports("unknown") {
		t.Fatal("unknown model should not be supported")
	}
	_, err = provider.Chat(context.Background(), ChatRequest{
		Model:    "unknown",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	})
	if !errors.Is(err, ErrUnsupportedModel) {
		t.Fatalf("expected ErrUnsupportedModel, got %v", err)
	}
}

func TestMockProviderUsesConfiguredRolesAndResponse(t *testing.T) {
	provider, err := NewMockProvider(testConfig())
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	resp, err := provider.Chat(context.Background(), ChatRequest{
		Model: "mock-gpt",
		Messages: []ChatMessage{
			{Role: "system", Content: "be brief"},
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if resp.ID != "chatcmpl-test" {
		t.Fatalf("unexpected response id: %q", resp.ID)
	}
	if resp.Choices[0].Message.Role != "assistant" {
		t.Fatalf("unexpected assistant role: %q", resp.Choices[0].Message.Role)
	}
	if resp.Choices[0].Message.Content != "Configured response: hello" {
		t.Fatalf("unexpected content: %q", resp.Choices[0].Message.Content)
	}
	if resp.Choices[0].FinishReason != "done" {
		t.Fatalf("unexpected finish reason: %q", resp.Choices[0].FinishReason)
	}
}
