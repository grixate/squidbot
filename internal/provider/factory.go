package provider

import (
	"fmt"
	"strings"

	"github.com/grixate/squidbot/internal/config"
)

func FromConfig(cfg config.Config) (LLMProvider, string, error) {
	name, p := cfg.PrimaryProvider()
	model := cfg.Agents.Defaults.Model

	switch name {
	case "openrouter":
		base := p.APIBase
		if strings.TrimSpace(base) == "" {
			base = "https://openrouter.ai/api/v1"
		}
		return NewOpenAICompatProvider(p.APIKey, base), model, nil
	case "openai":
		base := p.APIBase
		if strings.TrimSpace(base) == "" {
			base = "https://api.openai.com/v1"
		}
		if strings.TrimSpace(p.Model) != "" {
			model = p.Model
		}
		return NewOpenAICompatProvider(p.APIKey, base), model, nil
	case "anthropic":
		if strings.TrimSpace(p.Model) != "" {
			model = p.Model
		}
		return NewAnthropicProvider(p.APIKey, model), model, nil
	default:
		return nil, "", fmt.Errorf("no provider configured")
	}
}
