package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/agent"
	"github.com/grixate/squidbot/internal/budget"
	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/provider"
	storepkg "github.com/grixate/squidbot/internal/storage/bbolt"
	"github.com/grixate/squidbot/internal/subagent"
)

type tokenSafetyProvider struct {
	mu    sync.Mutex
	mode  string
	calls int
}

func (p *tokenSafetyProvider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{SupportsTools: true}
}

func (p *tokenSafetyProvider) Stream(ctx context.Context, req provider.ChatRequest) (<-chan provider.StreamEvent, <-chan error) {
	events := make(chan provider.StreamEvent)
	errs := make(chan error, 1)
	close(events)
	close(errs)
	return events, errs
}

func (p *tokenSafetyProvider) Chat(ctx context.Context, req provider.ChatRequest) (provider.ChatResponse, error) {
	p.mu.Lock()
	call := p.calls
	p.calls++
	mode := p.mode
	p.mu.Unlock()

	if isSubagentRequest(req.Messages) {
		return provider.ChatResponse{Content: "subagent done", Usage: provider.Usage{TotalTokens: 1}}, nil
	}

	switch mode {
	case "trusted_set_limits":
		switch call {
		case 0:
			args, _ := json.Marshal(map[string]any{"global_hard_limit_tokens": 17})
			return provider.ChatResponse{ToolCalls: []provider.ToolCall{{ID: "set-1", Name: "budget_set_limits", Arguments: args}}, Usage: provider.Usage{TotalTokens: 1}}, nil
		case 1:
			args, _ := json.Marshal(map[string]any{})
			return provider.ChatResponse{ToolCalls: []provider.ToolCall{{ID: "st-1", Name: "budget_status", Arguments: args}}, Usage: provider.Usage{TotalTokens: 1}}, nil
		default:
			return provider.ChatResponse{Content: "done", Usage: provider.Usage{TotalTokens: 1}}, nil
		}
	case "untrusted_set_enabled":
		if call == 0 {
			args, _ := json.Marshal(map[string]any{"enabled": false})
			return provider.ChatResponse{ToolCalls: []provider.ToolCall{{ID: "se-1", Name: "budget_set_enabled", Arguments: args}}, Usage: provider.Usage{TotalTokens: 1}}, nil
		}
		return provider.ChatResponse{Content: "done", Usage: provider.Usage{TotalTokens: 1}}, nil
	case "disable_then_continue":
		if call == 0 {
			args, _ := json.Marshal(map[string]any{"enabled": false})
			return provider.ChatResponse{ToolCalls: []provider.ToolCall{{ID: "sd-1", Name: "budget_set_enabled", Arguments: args}}, Usage: provider.Usage{TotalTokens: 1}}, nil
		}
		if call == 1 {
			return provider.ChatResponse{Content: "disabled", Usage: provider.Usage{TotalTokens: 1}}, nil
		}
		return provider.ChatResponse{Content: "after disable", Usage: provider.Usage{TotalTokens: 1}}, nil
	case "subagent_block":
		if call == 0 {
			args, _ := json.Marshal(map[string]any{"task": "should block", "wait": true})
			return provider.ChatResponse{ToolCalls: []provider.ToolCall{{ID: "sp-1", Name: "spawn", Arguments: args}}, Usage: provider.Usage{TotalTokens: 1}}, nil
		}
		return provider.ChatResponse{Content: "done", Usage: provider.Usage{TotalTokens: 1}}, nil
	default:
		return provider.ChatResponse{Content: "ok", Usage: provider.Usage{TotalTokens: 1}}, nil
	}
}

func (p *tokenSafetyProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func TestBudgetToolsTrustedWriterCanUpdateAndReadStatus(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Agents.Defaults.MaxTokens = 1
	cfg.Runtime.TokenSafety.GlobalHardLimitTokens = 100
	store, err := storepkg.Open(filepath.Join(t.TempDir(), "token-trusted.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	providerStub := &tokenSafetyProvider{mode: "trusted_set_limits"}
	engine, err := agent.NewEngine(cfg, providerStub, "test-model", store, nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	resp, err := engine.Ask(context.Background(), agent.InboundMessage{
		SessionID: "cli:budget1",
		Channel:   "cli",
		ChatID:    "direct",
		SenderID:  "user",
		Content:   "update budget",
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp != "done" {
		t.Fatalf("unexpected response: %q", resp)
	}
	override, err := store.GetTokenSafetyOverride(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if override.Settings.GlobalHardLimitTokens != 17 {
		t.Fatalf("expected override global hard limit 17, got %d", override.Settings.GlobalHardLimitTokens)
	}
	if providerStub.callCount() < 3 {
		t.Fatalf("expected provider tool/status round-trip, calls=%d", providerStub.callCount())
	}
}

func TestBudgetToolsRejectUntrustedWriter(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Agents.Defaults.MaxTokens = 1
	store, err := storepkg.Open(filepath.Join(t.TempDir(), "token-untrusted.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	providerStub := &tokenSafetyProvider{mode: "untrusted_set_enabled"}
	engine, err := agent.NewEngine(cfg, providerStub, "test-model", store, nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	_, err = engine.Ask(context.Background(), agent.InboundMessage{
		SessionID: "cli:budget2",
		Channel:   "cli",
		ChatID:    "direct",
		SenderID:  "intruder",
		Content:   "disable budget",
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.GetTokenSafetyOverride(context.Background())
	if !errors.Is(err, budget.ErrNotFound) {
		t.Fatalf("expected no override for unauthorized writer, got %v", err)
	}
}

func TestTokenSafetyGlobalHardLimitBlocksParentAndDisableBypassesImmediately(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Agents.Defaults.MaxTokens = 2
	cfg.Runtime.TokenSafety.GlobalHardLimitTokens = 1
	cfg.Runtime.TokenSafety.SessionHardLimitTokens = 10
	cfg.Runtime.TokenSafety.SubagentRunHardLimitTokens = 10
	store, err := storepkg.Open(filepath.Join(t.TempDir(), "token-block-parent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	providerStub := &tokenSafetyProvider{mode: "default"}
	engine, err := agent.NewEngine(cfg, providerStub, "test-model", store, nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	resp, err := engine.Ask(context.Background(), agent.InboundMessage{
		SessionID: "cli:budget3",
		Channel:   "cli",
		ChatID:    "direct",
		SenderID:  "user",
		Content:   "hello",
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp, "Token safety blocked this request") {
		t.Fatalf("expected hard-block response, got %q", resp)
	}
	if providerStub.callCount() != 0 {
		t.Fatalf("expected no provider call due to preflight block, got %d", providerStub.callCount())
	}

	cfg.Runtime.TokenSafety.GlobalHardLimitTokens = 1
	cfg.Agents.Defaults.MaxTokens = 1
	providerStub = &tokenSafetyProvider{mode: "disable_then_continue"}
	engine2, err := agent.NewEngine(cfg, providerStub, "test-model", store, nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer engine2.Close()
	_, err = engine2.Ask(context.Background(), agent.InboundMessage{
		SessionID: "cli:budget4",
		Channel:   "cli",
		ChatID:    "direct",
		SenderID:  "user",
		Content:   "disable",
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err = engine2.Ask(context.Background(), agent.InboundMessage{
		SessionID: "cli:budget4",
		Channel:   "cli",
		ChatID:    "direct",
		SenderID:  "user",
		Content:   "continue",
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp != "after disable" {
		t.Fatalf("expected bypass response after disable, got %q", resp)
	}
}

func TestTokenSafetySubagentHardLimitBlocksRun(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Agents.Defaults.MaxTokens = 2
	cfg.Runtime.TokenSafety.GlobalHardLimitTokens = 100
	cfg.Runtime.TokenSafety.SessionHardLimitTokens = 100
	cfg.Runtime.TokenSafety.SubagentRunHardLimitTokens = 1
	cfg.Runtime.Subagents.NotifyOnComplete = false

	store, err := storepkg.Open(filepath.Join(t.TempDir(), "token-block-subagent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	providerStub := &tokenSafetyProvider{mode: "subagent_block"}
	engine, err := agent.NewEngine(cfg, providerStub, "test-model", store, nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	_, err = engine.Ask(context.Background(), agent.InboundMessage{
		SessionID: "cli:budget5",
		Channel:   "cli",
		ChatID:    "direct",
		SenderID:  "user",
		Content:   "spawn",
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	runs, err := store.ListSubagentRunsBySession(context.Background(), "cli:budget5", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected one subagent run, got %d", len(runs))
	}
	if runs[0].Status != subagent.StatusFailed {
		t.Fatalf("expected failed status for budget-blocked subagent, got %s", runs[0].Status)
	}
	if !strings.Contains(runs[0].Error, "budget_exhausted") {
		t.Fatalf("expected budget_exhausted error, got %q", runs[0].Error)
	}
}
