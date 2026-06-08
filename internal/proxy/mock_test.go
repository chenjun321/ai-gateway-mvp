package proxy

import (
	"context"
	"errors"
	"testing"
)

func TestMockProviderRejectsUnconfiguredModel(t *testing.T) {
	provider, err := NewMockProvider([]ModelConfig{{Name: "mock-gpt", Behavior: ModelBehaviorSuccess}})
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
