package config

import (
	"fmt"
	"strings"
)

type OnboardingInput struct {
	Provider string
	ProviderConfig
	Telegram TelegramConfig
}

func ApplyOnboardingInput(cfg Config, input OnboardingInput) (Config, error) {
	providerName, ok := NormalizeProviderName(input.Provider)
	if !ok {
		return cfg, fmt.Errorf("unsupported provider %q (supported: %s)", input.Provider, strings.Join(SupportedProviders(), ", "))
	}
	providerCfg := input.ProviderConfig
	if strings.TrimSpace(providerCfg.APIBase) == "" {
		if base := ProviderDefaultAPIBase(providerName); base != "" {
			providerCfg.APIBase = base
		}
	}
	if strings.TrimSpace(providerCfg.Model) == "" {
		if model := ProviderDefaultModel(providerName); model != "" {
			providerCfg.Model = model
		}
	}

	cfg.Providers.Active = providerName
	_ = cfg.SetProviderByName(providerName, providerCfg)
	cfg.Channels.Telegram = TelegramConfig{
		Enabled:   input.Telegram.Enabled,
		Token:     strings.TrimSpace(input.Telegram.Token),
		AllowFrom: normalizeAllowFrom(input.Telegram.AllowFrom),
	}

	if err := ValidateActiveProvider(cfg); err != nil {
		return cfg, err
	}
	if err := validateTelegramOnboarding(cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func ValidateProviderDraft(provider string, providerCfg ProviderConfig) error {
	name, ok := NormalizeProviderName(provider)
	if !ok {
		return fmt.Errorf("unsupported provider %q", provider)
	}
	cfg := Default()
	cfg.Providers.Active = name
	_ = cfg.SetProviderByName(name, providerCfg)
	return ValidateActiveProvider(cfg)
}
