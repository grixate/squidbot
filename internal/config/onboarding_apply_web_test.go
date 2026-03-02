package config

import "testing"

func TestApplyWebOnboardingInputPersistsMultipleProviders(t *testing.T) {
	cfg := Default()
	next, err := ApplyWebOnboardingInput(cfg, WebOnboardingInput{
		Providers: []WebOnboardingProviderInput{
			{ID: ProviderOllama, ProviderConfig: ProviderConfig{Model: "llama3.1:8b"}},
			{ID: ProviderOpenAI, ProviderConfig: ProviderConfig{APIKey: "test-key"}},
		},
		ActiveProviderID: ProviderOpenAI,
		PasswordHash:     "hash",
	})
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if next.Providers.Active != ProviderOpenAI {
		t.Fatalf("expected active provider %q, got %q", ProviderOpenAI, next.Providers.Active)
	}
	if _, ok := next.ProviderByName(ProviderOllama); !ok {
		t.Fatal("expected ollama provider to be configured")
	}
	if providerCfg, ok := next.ProviderByName(ProviderOpenAI); !ok || providerCfg.APIKey != "test-key" {
		t.Fatal("expected openai provider to be configured")
	}
	if next.Auth.PasswordHash != "hash" {
		t.Fatal("expected password hash to be persisted")
	}
	if next.Channels.Telegram.Enabled {
		t.Fatal("expected telegram to remain disabled when omitted")
	}
}

func TestApplyWebOnboardingInputRejectsInvalidActiveProvider(t *testing.T) {
	cfg := Default()
	_, err := ApplyWebOnboardingInput(cfg, WebOnboardingInput{
		Providers: []WebOnboardingProviderInput{
			{ID: ProviderOllama, ProviderConfig: ProviderConfig{Model: "llama3.1:8b"}},
		},
		ActiveProviderID: ProviderOpenAI,
		PasswordHash:     "hash",
	})
	if err == nil {
		t.Fatal("expected invalid active provider error")
	}
}
