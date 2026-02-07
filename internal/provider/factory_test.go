package provider

import (
	"testing"

	"github.com/grixate/squidbot/internal/config"
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
}
