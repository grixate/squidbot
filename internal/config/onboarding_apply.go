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

type WebOnboardingProviderInput struct {
	ID string `json:"id"`
	ProviderConfig
}

type WebOnboardingInput struct {
	Providers         []WebOnboardingProviderInput `json:"providers"`
	ActiveProviderID  string                       `json:"activeProviderId"`
	Telegram          *TelegramConfig              `json:"telegram,omitempty"`
	PasswordHash      string                       `json:"passwordHash"`
	PasswordUpdatedAt string                       `json:"passwordUpdatedAt,omitempty"`
}

func ApplyOnboardingInput(cfg Config, input OnboardingInput) (Config, error) {
	providerName, ok := NormalizeProviderName(input.Provider)
	if !ok {
		return cfg, fmt.Errorf("unsupported provider %q (supported: %s)", input.Provider, strings.Join(SupportedProviders(), ", "))
	}
	providerCfg := providerConfigWithDefaults(providerName, input.ProviderConfig)

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
	applyTelegramConfig(&cfg, input.Telegram)
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

func ApplyWebOnboardingInput(cfg Config, input WebOnboardingInput) (Config, error) {
	if len(input.Providers) == 0 {
		return cfg, fmt.Errorf("at least one provider is required")
	}
	activeProviderID, ok := NormalizeProviderName(input.ActiveProviderID)
	if !ok {
		return cfg, fmt.Errorf("activeProviderId must be one of: %s", strings.Join(SupportedProviders(), ", "))
	}

	resetConfiguredProviders(&cfg)

	seen := map[string]struct{}{}
	activeFound := false
	for _, draft := range input.Providers {
		providerName, ok := NormalizeProviderName(draft.ID)
		if !ok {
			return cfg, fmt.Errorf("unsupported provider %q (supported: %s)", draft.ID, strings.Join(SupportedProviders(), ", "))
		}
		if _, exists := seen[providerName]; exists {
			return cfg, fmt.Errorf("duplicate provider %q", providerName)
		}
		seen[providerName] = struct{}{}

		providerCfg := providerConfigWithDefaults(providerName, draft.ProviderConfig)
		if err := validateProviderConfig(providerName, providerCfg); err != nil {
			return cfg, err
		}
		_ = cfg.SetProviderByName(providerName, providerCfg)
		if providerName == activeProviderID {
			activeFound = true
		}
	}
	if !activeFound {
		return cfg, fmt.Errorf("activeProviderId must match one of the submitted providers")
	}

	cfg.Providers.Active = activeProviderID
	if input.Telegram != nil {
		applyTelegramConfig(&cfg, *input.Telegram)
	} else {
		applyTelegramConfig(&cfg, TelegramConfig{Enabled: false, AllowFrom: []string{}})
	}
	migrateLegacyChannels(&cfg)

	if strings.TrimSpace(input.PasswordHash) != "" {
		cfg.Auth.PasswordHash = strings.TrimSpace(input.PasswordHash)
		cfg.Auth.PasswordUpdatedAt = strings.TrimSpace(input.PasswordUpdatedAt)
	}

	if err := ValidateActiveProvider(cfg); err != nil {
		return cfg, err
	}
	if err := validateTelegramOnboarding(cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func providerConfigWithDefaults(providerName string, providerCfg ProviderConfig) ProviderConfig {
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
	return providerCfg
}

func applyTelegramConfig(cfg *Config, telegram TelegramConfig) {
	cfg.Channels.Telegram = TelegramConfig{
		Enabled:   telegram.Enabled,
		Token:     strings.TrimSpace(telegram.Token),
		AllowFrom: normalizeAllowFrom(telegram.AllowFrom),
	}
	if cfg.Channels.Registry == nil {
		cfg.Channels.Registry = map[string]GenericChannelConfig{}
	}
	cfg.Channels.Registry["telegram"] = GenericChannelConfig{
		Label:     "Telegram",
		Kind:      "core",
		Enabled:   cfg.Channels.Telegram.Enabled,
		Token:     cfg.Channels.Telegram.Token,
		AllowFrom: cfg.Channels.Telegram.AllowFrom,
	}
}

func resetConfiguredProviders(cfg *Config) {
	cfg.Providers.Registry = map[string]ProviderConfig{}
	cfg.Providers.OpenRouter = ProviderConfig{}
	cfg.Providers.Anthropic = ProviderConfig{}
	cfg.Providers.OpenAI = ProviderConfig{}
	cfg.Providers.Gemini = ProviderConfig{}
	cfg.Providers.Ollama = ProviderConfig{}
	cfg.Providers.LMStudio = ProviderConfig{}
}
