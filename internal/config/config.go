package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

const DefaultPath = "gateway-config.json"

type Config struct {
	Scopes          ScopeConfig           `json:"scopes"`
	Chat            ChatConfig            `json:"chat"`
	Mock            MockConfig            `json:"mock"`
	UsageEstimation UsageEstimationConfig `json:"usage_estimation"`
	Models          []ModelConfig         `json:"models"`
}

type ScopeConfig struct {
	ChatInvoke  string `json:"chat_invoke"`
	ModelPrefix string `json:"model_prefix"`
}

type ChatConfig struct {
	AllowedRoles  []string `json:"allowed_roles"`
	UserRole      string   `json:"user_role"`
	AssistantRole string   `json:"assistant_role"`
}

type MockConfig struct {
	ResponseID          string `json:"response_id"`
	ResponseObject      string `json:"response_object"`
	ResponsePrefix      string `json:"response_prefix"`
	FallbackUserContent string `json:"fallback_user_content"`
	FinishReason        string `json:"finish_reason"`
}

type UsageEstimationConfig struct {
	CharsPerToken         int `json:"chars_per_token"`
	MessageOverheadTokens int `json:"message_overhead_tokens"`
	PromptOverheadTokens  int `json:"prompt_overhead_tokens"`
}

type ModelConfig struct {
	Name     string `json:"name"`
	Behavior string `json:"behavior"`
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("decode gateway config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) Validate() error {
	c.Scopes.ChatInvoke = strings.TrimSpace(c.Scopes.ChatInvoke)
	c.Scopes.ModelPrefix = strings.TrimSpace(c.Scopes.ModelPrefix)
	if c.Scopes.ChatInvoke == "" {
		return errors.New("scopes.chat_invoke is required")
	}
	if c.Scopes.ModelPrefix == "" {
		return errors.New("scopes.model_prefix is required")
	}
	c.Chat.UserRole = strings.TrimSpace(c.Chat.UserRole)
	c.Chat.AssistantRole = strings.TrimSpace(c.Chat.AssistantRole)
	if c.Chat.UserRole == "" {
		return errors.New("chat.user_role is required")
	}
	if c.Chat.AssistantRole == "" {
		return errors.New("chat.assistant_role is required")
	}
	if len(c.Chat.AllowedRoles) == 0 {
		return errors.New("chat.allowed_roles must include at least one role")
	}
	roleSet := map[string]bool{}
	for i := range c.Chat.AllowedRoles {
		c.Chat.AllowedRoles[i] = strings.TrimSpace(c.Chat.AllowedRoles[i])
		if c.Chat.AllowedRoles[i] == "" {
			return errors.New("chat.allowed_roles cannot contain an empty role")
		}
		if roleSet[c.Chat.AllowedRoles[i]] {
			return fmt.Errorf("duplicate chat role %q", c.Chat.AllowedRoles[i])
		}
		roleSet[c.Chat.AllowedRoles[i]] = true
	}
	if !roleSet[c.Chat.UserRole] {
		return fmt.Errorf("chat.user_role %q must be listed in chat.allowed_roles", c.Chat.UserRole)
	}
	if !roleSet[c.Chat.AssistantRole] {
		return fmt.Errorf("chat.assistant_role %q must be listed in chat.allowed_roles", c.Chat.AssistantRole)
	}
	c.Mock.ResponseID = strings.TrimSpace(c.Mock.ResponseID)
	c.Mock.ResponseObject = strings.TrimSpace(c.Mock.ResponseObject)
	c.Mock.FallbackUserContent = strings.TrimSpace(c.Mock.FallbackUserContent)
	c.Mock.FinishReason = strings.TrimSpace(c.Mock.FinishReason)
	if c.Mock.ResponseID == "" {
		return errors.New("mock.response_id is required")
	}
	if c.Mock.ResponseObject == "" {
		return errors.New("mock.response_object is required")
	}
	if c.Mock.FallbackUserContent == "" {
		return errors.New("mock.fallback_user_content is required")
	}
	if c.Mock.FinishReason == "" {
		return errors.New("mock.finish_reason is required")
	}
	if c.UsageEstimation.CharsPerToken <= 0 {
		return errors.New("usage_estimation.chars_per_token must be positive")
	}
	if c.UsageEstimation.MessageOverheadTokens < 0 {
		return errors.New("usage_estimation.message_overhead_tokens cannot be negative")
	}
	if c.UsageEstimation.PromptOverheadTokens < 0 {
		return errors.New("usage_estimation.prompt_overhead_tokens cannot be negative")
	}
	if len(c.Models) == 0 {
		return errors.New("config must include at least one model")
	}
	seen := map[string]bool{}
	for i := range c.Models {
		c.Models[i].Name = strings.TrimSpace(c.Models[i].Name)
		c.Models[i].Behavior = strings.TrimSpace(c.Models[i].Behavior)
		if c.Models[i].Name == "" {
			return errors.New("model name is required")
		}
		if seen[c.Models[i].Name] {
			return fmt.Errorf("duplicate model %q", c.Models[i].Name)
		}
		seen[c.Models[i].Name] = true
	}
	return nil
}

func (c Config) ModelScope(model string) string {
	return c.Scopes.ModelPrefix + model
}

func (c Config) ModelWildcardScope() string {
	return c.Scopes.ModelPrefix + "*"
}

func (c Config) ModelNames() []string {
	models := make([]string, 0, len(c.Models))
	for _, model := range c.Models {
		models = append(models, model.Name)
	}
	return models
}

func (c Config) DefaultTenantScopes() []string {
	scopes := []string{c.Scopes.ChatInvoke}
	if len(c.Models) > 0 {
		scopes = append(scopes, c.ModelScope(c.Models[0].Name))
	}
	return scopes
}

func (c Config) AllowsRole(role string) bool {
	for _, allowed := range c.Chat.AllowedRoles {
		if role == allowed {
			return true
		}
	}
	return false
}
