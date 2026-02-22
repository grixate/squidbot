package config

import (
	"strings"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/oauth"
)

func TestValidateActiveProvider(t *testing.T) {
	t.Run("missing active and no legacy provider", func(t *testing.T) {
		cfg := Default()
		cfg.Providers.Active = ""
		if err := ValidateActiveProvider(cfg); err == nil {
			t.Fatal("expected error for missing provider")
		}
	})

	t.Run("invalid active", func(t *testing.T) {
		cfg := Default()
		cfg.Providers.Active = "invalid-provider"
		if err := ValidateActiveProvider(cfg); err == nil {
			t.Fatal("expected error for invalid provider")
		}
	})

	t.Run("gemini missing key", func(t *testing.T) {
		cfg := Default()
		cfg.Providers.Active = ProviderGemini
		cfg.Providers.Gemini.Model = "gemini-3.0-pro"
		if err := ValidateActiveProvider(cfg); err == nil {
			t.Fatal("expected error for gemini missing api key")
		}
	})

	t.Run("ollama without key is valid when model is set", func(t *testing.T) {
		cfg := Default()
		cfg.Providers.Active = ProviderOllama
		cfg.Providers.Ollama.Model = "llama3.1:8b"
		if err := ValidateActiveProvider(cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("lmstudio missing model", func(t *testing.T) {
		cfg := Default()
		cfg.Providers.Active = ProviderLMStudio
		if err := ValidateActiveProvider(cfg); err == nil {
			t.Fatal("expected error for lmstudio missing model")
		}
	})

	t.Run("legacy fallback openai key", func(t *testing.T) {
		cfg := Default()
		cfg.Providers.Active = ""
		cfg.Providers.OpenAI.APIKey = "test-key"
		if err := ValidateActiveProvider(cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("moonshot missing key", func(t *testing.T) {
		cfg := Default()
		cfg.Providers.Active = "moonshot"
		if err := ValidateActiveProvider(cfg); err == nil {
			t.Fatal("expected error for moonshot missing api key")
		}
	})

	t.Run("minimax missing key", func(t *testing.T) {
		cfg := Default()
		cfg.Providers.Active = "minimax"
		if err := ValidateActiveProvider(cfg); err == nil {
			t.Fatal("expected error for minimax missing api key")
		}
	})

	t.Run("moonshot with api key", func(t *testing.T) {
		cfg := Default()
		cfg.Providers.Active = "moonshot"
		_ = cfg.SetProviderByName("moonshot", ProviderConfig{APIKey: "test-key"})
		if err := ValidateActiveProvider(cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("minimax with api key", func(t *testing.T) {
		cfg := Default()
		cfg.Providers.Active = "minimax"
		_ = cfg.SetProviderByName("minimax", ProviderConfig{APIKey: "test-key"})
		if err := ValidateActiveProvider(cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("openai codex requires oauth token", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		cfg := Default()
		cfg.Providers.Active = ProviderOpenAICodex
		_ = cfg.SetProviderByName(ProviderOpenAICodex, ProviderConfig{Model: ProviderOpenAICodexDefaultModel})
		err := ValidateActiveProvider(cfg)
		if err == nil || !strings.Contains(err.Error(), "provider \"openai-codex\" requires oauth login") {
			t.Fatalf("expected oauth login requirement, got %v", err)
		}
	})

	t.Run("openai codex valid when token present", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		if err := oauth.NewOpenAICodexTokenStore().Save(oauth.Token{
			AccessToken:  "tok",
			RefreshToken: "ref",
			ExpiresAt:    time.Now().UTC().Add(time.Hour),
		}); err != nil {
			t.Fatalf("save token: %v", err)
		}
		cfg := Default()
		cfg.Providers.Active = ProviderOpenAICodex
		_ = cfg.SetProviderByName(ProviderOpenAICodex, ProviderConfig{Model: ProviderOpenAICodexDefaultModel})
		if err := ValidateActiveProvider(cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
