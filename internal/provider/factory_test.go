package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/oauth"
)

func TestFromConfig(t *testing.T) {
	t.Run("gemini defaults to documented base and provider model", func(t *testing.T) {
		cfg := config.Default()
		cfg.Agents.Defaults.Model = "agent-default"
		cfg.Providers.Active = config.ProviderGemini
		cfg.Providers.Gemini = config.ProviderConfig{
			APIKey: "gemini-key",
			Model:  "gemini-3.0-flash",
		}

		client, model, err := FromConfig(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		openaiCompat, ok := client.(*OpenAICompatProvider)
		if !ok {
			t.Fatalf("expected OpenAICompatProvider, got %T", client)
		}
		if openaiCompat.baseURL != config.ProviderDefaultAPIBase(config.ProviderGemini) {
			t.Fatalf("unexpected base URL: %s", openaiCompat.baseURL)
		}
		if model != "gemini-3.0-flash" {
			t.Fatalf("unexpected model: %s", model)
		}
	})

	t.Run("ollama supports empty api key", func(t *testing.T) {
		cfg := config.Default()
		cfg.Providers.Active = config.ProviderOllama
		cfg.Providers.Ollama = config.ProviderConfig{
			Model: "llama3.1:8b",
		}

		client, model, err := FromConfig(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		openaiCompat, ok := client.(*OpenAICompatProvider)
		if !ok {
			t.Fatalf("expected OpenAICompatProvider, got %T", client)
		}
		if openaiCompat.baseURL != config.ProviderDefaultAPIBase(config.ProviderOllama) {
			t.Fatalf("unexpected base URL: %s", openaiCompat.baseURL)
		}
		if model != "llama3.1:8b" {
			t.Fatalf("unexpected model: %s", model)
		}
	})

	t.Run("lmstudio custom base is honored", func(t *testing.T) {
		cfg := config.Default()
		cfg.Providers.Active = config.ProviderLMStudio
		cfg.Providers.LMStudio = config.ProviderConfig{
			APIBase: "http://127.0.0.1:2233/v1",
			Model:   "my-local-model",
		}

		client, model, err := FromConfig(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		openaiCompat, ok := client.(*OpenAICompatProvider)
		if !ok {
			t.Fatalf("expected OpenAICompatProvider, got %T", client)
		}
		if openaiCompat.baseURL != "http://127.0.0.1:2233/v1" {
			t.Fatalf("unexpected base URL: %s", openaiCompat.baseURL)
		}
		if model != "my-local-model" {
			t.Fatalf("unexpected model: %s", model)
		}
	})

	t.Run("legacy fallback still works", func(t *testing.T) {
		cfg := config.Default()
		cfg.Providers.Active = ""
		cfg.Providers.OpenAI = config.ProviderConfig{
			APIKey: "openai-key",
			Model:  "gpt-4.1",
		}

		client, model, err := FromConfig(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := client.(*OpenAICompatProvider); !ok {
			t.Fatalf("expected OpenAICompatProvider, got %T", client)
		}
		if model != "gpt-4.1" {
			t.Fatalf("unexpected model: %s", model)
		}
	})

	t.Run("invalid config returns validation error", func(t *testing.T) {
		cfg := config.Default()
		cfg.Providers.Active = config.ProviderGemini
		cfg.Providers.Gemini = config.ProviderConfig{}

		_, _, err := FromConfig(cfg)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("moonshot uses default moonshot base", func(t *testing.T) {
		cfg := config.Default()
		cfg.Providers.Active = "moonshot"
		_ = cfg.SetProviderByName("moonshot", config.ProviderConfig{
			APIKey: "moonshot-key",
		})

		client, _, err := FromConfig(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		openaiCompat, ok := client.(*OpenAICompatProvider)
		if !ok {
			t.Fatalf("expected OpenAICompatProvider, got %T", client)
		}
		if openaiCompat.baseURL != "https://api.moonshot.ai/v1" {
			t.Fatalf("unexpected base URL: %s", openaiCompat.baseURL)
		}
	})

	t.Run("minimax uses default minimax base", func(t *testing.T) {
		cfg := config.Default()
		cfg.Providers.Active = "minimax"
		_ = cfg.SetProviderByName("minimax", config.ProviderConfig{
			APIKey: "minimax-key",
		})

		client, _, err := FromConfig(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		openaiCompat, ok := client.(*OpenAICompatProvider)
		if !ok {
			t.Fatalf("expected OpenAICompatProvider, got %T", client)
		}
		if openaiCompat.baseURL != "https://api.minimax.io/v1" {
			t.Fatalf("unexpected base URL: %s", openaiCompat.baseURL)
		}
	})

	t.Run("openai codex requires feature flag", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		if err := oauth.NewOpenAICodexTokenStore().Save(oauth.Token{
			AccessToken:  "tok",
			RefreshToken: "ref",
			ExpiresAt:    time.Now().UTC().Add(time.Hour),
		}); err != nil {
			t.Fatalf("save token: %v", err)
		}
		cfg := config.Default()
		cfg.Providers.Active = config.ProviderOpenAICodex
		_ = cfg.SetProviderByName(config.ProviderOpenAICodex, config.ProviderConfig{
			Model: config.ProviderOpenAICodexDefaultModel,
		})
		_, _, err := FromConfig(cfg)
		if err == nil || !strings.Contains(err.Error(), "features.codexOAuth") {
			t.Fatalf("expected codex feature-flag error, got %v", err)
		}
	})

	t.Run("openai codex constructs dedicated provider", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		store := oauth.NewOpenAICodexTokenStore()
		if err := store.Save(oauth.Token{
			AccessToken:  "tok-1",
			RefreshToken: "ref-1",
			ExpiresAt:    time.Now().UTC().Add(time.Hour),
		}); err != nil {
			t.Fatalf("save token: %v", err)
		}
		cfg := config.Default()
		cfg.Features.CodexOAuth = true
		cfg.Providers.Active = config.ProviderOpenAICodex
		_ = cfg.SetProviderByName(config.ProviderOpenAICodex, config.ProviderConfig{
			Model:   config.ProviderOpenAICodexDefaultModel,
			APIBase: config.ProviderOpenAICodexDefaultAPIBase,
		})
		client, model, err := FromConfig(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := client.(*OpenAICodexProvider); !ok {
			t.Fatalf("expected OpenAICodexProvider, got %T", client)
		}
		if model != config.ProviderOpenAICodexDefaultModel {
			t.Fatalf("unexpected model: %s", model)
		}
		_ = os.Remove(filepath.Join(home, ".squidbot", "oauth", "openai-codex.json"))
	})
}
