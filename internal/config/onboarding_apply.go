package config

import (
	"fmt"
	"strings"
)

type OnboardingInput struct {
	Provider string
	ProviderConfig
	Telegram TelegramConfig
	Channels map[string]GenericChannelConfig
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
	if cfg.Channels.Registry == nil {
		cfg.Channels.Registry = map[string]GenericChannelConfig{}
	}
	if len(input.Channels) > 0 {
		for id, channel := range input.Channels {
			normalizedID := strings.ToLower(strings.TrimSpace(id))
			if normalizedID == "" {
				continue
			}
			channel.Token = strings.TrimSpace(channel.Token)
			channel.AllowFrom = normalizeAllowFrom(channel.AllowFrom)
			cfg.Channels.Registry[normalizedID] = channel
		}
	}
	cfg.Channels.Registry["telegram"] = GenericChannelConfig{
		Label:     "Telegram",
		Kind:      "core",
		Enabled:   input.Telegram.Enabled,
		Token:     strings.TrimSpace(input.Telegram.Token),
		AllowFrom: normalizeAllowFrom(input.Telegram.AllowFrom),
	}
	migrateLegacyChannels(&cfg)

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
