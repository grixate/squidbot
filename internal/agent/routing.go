package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/provider"
	processkind "github.com/grixate/squidbot/internal/runtime/process"
)

func (e *Engine) primaryModelForProcess(kindRaw string, task string) string {
	models := e.modelsForProcess(kindRaw, task)
	if len(models) == 0 {
		_, fallback := e.currentProviderModel()
		return fallback
	}
	return models[0]
}

func (e *Engine) modelsForProcess(kindRaw string, task string) []string {
	cfg := e.currentConfig()
	kind := processkind.Normalize(kindRaw)
	_, baseModel := e.currentProviderModel()

	primary := strings.TrimSpace(baseModel)
	switch kind {
	case processkind.Branch:
		primary = defaultString(cfg.Runtime.Routing.BranchModel, primary)
	case processkind.Worker:
		primary = defaultString(cfg.Runtime.Routing.WorkerModel, primary)
	case processkind.Compactor:
		primary = defaultString(cfg.Runtime.Routing.CompactorModel, primary)
	case processkind.Cortex:
		primary = defaultString(cfg.Runtime.Routing.CortexModel, primary)
	default:
		primary = defaultString(cfg.Runtime.Routing.ChannelModel, primary)
	}
	task = strings.TrimSpace(strings.ToLower(task))
	if task != "" {
		if override, ok := cfg.Runtime.Routing.TaskOverrides[kind.String()+":"+task]; ok && strings.TrimSpace(override) != "" {
			primary = strings.TrimSpace(override)
		} else if override, ok := cfg.Runtime.Routing.TaskOverrides[task]; ok && strings.TrimSpace(override) != "" {
			primary = strings.TrimSpace(override)
		}
	}

	out := []string{}
	appendIf := func(model string) {
		model = strings.TrimSpace(model)
		if model == "" {
			return
		}
		for _, existing := range out {
			if existing == model {
				return
			}
		}
		out = append(out, model)
	}
	appendIf(primary)

	fallbackKey := kind.String()
	fallbacks := cfg.Runtime.Routing.Fallbacks[fallbackKey]
	if task != "" {
		if keyed, ok := cfg.Runtime.Routing.Fallbacks[fallbackKey+":"+task]; ok && len(keyed) > 0 {
			fallbacks = keyed
		}
	}
	for _, model := range fallbacks {
		appendIf(model)
	}
	appendIf(baseModel)
	return out
}

func (e *Engine) chatWithRouting(
	ctx context.Context,
	kindRaw string,
	task string,
	req provider.ChatRequest,
) (provider.ChatResponse, string, error) {
	providerClient, _ := e.currentProviderModel()
	if providerClient == nil {
		return provider.ChatResponse{}, "", fmt.Errorf("provider is not configured")
	}
	models := e.modelsForProcess(kindRaw, task)
	if len(models) == 0 {
		models = []string{strings.TrimSpace(req.Model)}
	}
	lastErr := error(nil)
	tried := 0
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if e.modelInCooldown(model) {
			continue
		}
		tried++
		req.Model = model
		e.metrics.ProviderCalls.Add(1)
		resp, err := providerClient.Chat(ctx, req)
		if err == nil {
			return resp, model, nil
		}
		e.metrics.ProviderErrors.Add(1)
		lastErr = err
		if isRateLimitError(err) {
			e.putModelCooldown(model)
		}
	}
	if tried == 0 && len(models) > 0 {
		model := strings.TrimSpace(models[0])
		if model != "" {
			req.Model = model
			e.metrics.ProviderCalls.Add(1)
			resp, err := providerClient.Chat(ctx, req)
			if err == nil {
				return resp, model, nil
			}
			e.metrics.ProviderErrors.Add(1)
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no model candidates available")
	}
	return provider.ChatResponse{}, "", lastErr
}

func (e *Engine) modelInCooldown(model string) bool {
	cfg := e.currentConfig()
	if cfg.Runtime.Routing.RateLimitCooldownSec <= 0 {
		return false
	}
	e.routingMu.Lock()
	defer e.routingMu.Unlock()
	until, ok := e.modelCooldown[strings.TrimSpace(model)]
	if !ok {
		return false
	}
	return time.Now().Before(until)
}

func (e *Engine) putModelCooldown(model string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	cfg := e.currentConfig()
	cooldown := time.Duration(max(cfg.Runtime.Routing.RateLimitCooldownSec, 0)) * time.Second
	if cooldown <= 0 {
		return
	}
	e.routingMu.Lock()
	e.modelCooldown[model] = time.Now().Add(cooldown)
	e.routingMu.Unlock()
}

func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	patterns := []string{
		"rate limit",
		"too many requests",
		"429",
		"overloaded",
		"capacity",
	}
	for _, pattern := range patterns {
		if strings.Contains(msg, pattern) {
			return true
		}
	}
	return false
}

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return strings.TrimSpace(fallback)
	}
	return value
}
