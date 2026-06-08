package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway-config.json")
	if err := os.WriteFile(path, []byte(`{
		"scopes": {
			"chat_invoke": "chat:invoke",
			"model_prefix": "model:"
		},
		"models": [
			{"name": "mock-gpt", "behavior": "success"},
			{"name": "mock-timeout", "behavior": "timeout"}
		]
	}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Scopes.ChatInvoke != "chat:invoke" {
		t.Fatalf("unexpected chat scope: %q", cfg.Scopes.ChatInvoke)
	}
	if got := cfg.ModelScope("mock-gpt"); got != "model:mock-gpt" {
		t.Fatalf("unexpected model scope: %q", got)
	}
	if got := cfg.DefaultTenantScopes(); len(got) != 2 || got[1] != "model:mock-gpt" {
		t.Fatalf("unexpected default scopes: %#v", got)
	}
}
