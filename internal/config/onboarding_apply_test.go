package config

import "testing"

func TestApplyOnboardingInputSetsDefaults(t *testing.T) {
	cfg := Default()
	next, err := ApplyOnboardingInput(cfg, OnboardingInput{
		Provider: ProviderOllama,
		ProviderConfig: ProviderConfig{
			Model: "llama3.1:8b",
		},
		Telegram: TelegramConfig{Enabled: false},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next.Providers.Active != ProviderOllama {
		t.Fatalf("unexpected active provider: %s", next.Providers.Active)
	}
	if next.Providers.Ollama.APIBase != ProviderDefaultAPIBase(ProviderOllama) {
		t.Fatalf("unexpected API base: %s", next.Providers.Ollama.APIBase)
	}
}

func TestApplyOnboardingInputTelegramRequiresTokenWhenEnabled(t *testing.T) {
	cfg := Default()
	_, err := ApplyOnboardingInput(cfg, OnboardingInput{
		Provider: ProviderOllama,
		ProviderConfig: ProviderConfig{
			Model: "llama3.1:8b",
		},
		Telegram: TelegramConfig{Enabled: true},
	})
	if err == nil {
		t.Fatal("expected telegram validation error")
	}
}

func TestValidateProviderDraft(t *testing.T) {
	if err := ValidateProviderDraft(ProviderGemini, ProviderConfig{APIKey: "sk-test"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ValidateProviderDraft("bad-provider", ProviderConfig{}); err == nil {
		t.Fatal("expected invalid provider error")
	}
}
