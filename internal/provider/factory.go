package provider

import (
	"fmt"
	"strings"

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

	switch name {
	case config.ProviderOpenRouter:
		base := p.APIBase
		if strings.TrimSpace(base) == "" {
			base = config.ProviderDefaultAPIBase(config.ProviderOpenRouter)
		}
		return NewOpenAICompatProvider(p.APIKey, base), model, nil
	case config.ProviderOpenAI:
		base := p.APIBase
		if strings.TrimSpace(base) == "" {
			base = config.ProviderDefaultAPIBase(config.ProviderOpenAI)
		}
		return NewOpenAICompatProvider(p.APIKey, base), model, nil
	case config.ProviderGemini:
		base := p.APIBase
		if strings.TrimSpace(base) == "" {
			base = config.ProviderDefaultAPIBase(config.ProviderGemini)
		}
		return NewOpenAICompatProvider(p.APIKey, base), model, nil
	case config.ProviderOllama:
		base := p.APIBase
		if strings.TrimSpace(base) == "" {
			base = config.ProviderDefaultAPIBase(config.ProviderOllama)
		}
		return NewOpenAICompatProvider(p.APIKey, base), model, nil
	case config.ProviderLMStudio:
		base := p.APIBase
		if strings.TrimSpace(base) == "" {
			base = config.ProviderDefaultAPIBase(config.ProviderLMStudio)
		}
		return NewOpenAICompatProvider(p.APIKey, base), model, nil
	case config.ProviderAnthropic:
		return NewAnthropicProvider(p.APIKey, model), model, nil
	default:
		return nil, "", fmt.Errorf("no provider configured")
	}
}
