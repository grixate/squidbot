package config

import "testing"

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
}
