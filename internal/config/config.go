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
	Scopes ScopeConfig   `json:"scopes"`
	Models []ModelConfig `json:"models"`
}

type ScopeConfig struct {
	ChatInvoke  string `json:"chat_invoke"`
	ModelPrefix string `json:"model_prefix"`
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
