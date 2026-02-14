package provider

import (
	"fmt"
	"strings"

	"github.com/grixate/squidbot/internal/catalog"
	"github.com/grixate/squidbot/internal/config"
)

func FromConfig(cfg config.Config) (LLMProvider, string, error) {
	if err := config.ValidateActiveProvider(cfg); err != nil {
		return nil, "", fmt.Errorf("provider configuration invalid: %w", err)
	}
	name, p := cfg.PrimaryProvider()
	model := cfg.Agents.Defaults.Model
	if strings.TrimSpace(p.Model) != "" {
		model = p.Model
	}

	profile, hasProfile := catalog.ProviderByID(name)
	if !hasProfile {
		profile = catalog.ProviderProfile{
			ID:           name,
			Transport:    "openai_compat",
			APIKeyHeader: "Authorization",
			APIKeyPrefix: "Bearer ",
		}
	}
	switch strings.TrimSpace(profile.Transport) {
	case "anthropic":
		return NewAnthropicProvider(p.APIKey, model), model, nil
	case "openai_compat", "":
		base := p.APIBase
		if strings.TrimSpace(base) == "" {
			base = config.ProviderDefaultAPIBase(name)
		}
		return NewOpenAICompatProviderWithOptions(p.APIKey, base, profile.APIKeyHeader, profile.APIKeyPrefix, nil), model, nil
	default:
		return nil, "", fmt.Errorf("unsupported provider transport %q for %q", profile.Transport, name)
	}
}
