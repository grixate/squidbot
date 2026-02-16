package agent_test

import (
	"context"
	"errors"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/agent"
	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/provider"
	storepkg "github.com/grixate/squidbot/internal/storage/bbolt"
)

type contextTestProvider struct {
	failSummary           bool
	failFirstOverflowMain bool
	mainCalls             int
	summaryCalls          int
	requests              []provider.ChatRequest
}

func (p *contextTestProvider) Chat(ctx context.Context, req provider.ChatRequest) (provider.ChatResponse, error) {
	p.requests = append(p.requests, req)
	if isSummaryRequest(req) {
		p.summaryCalls++
		if p.failSummary {
			return provider.ChatResponse{}, errors.New("summary unavailable")
		}
		return provider.ChatResponse{Content: "- summarized context"}, nil
	}
	p.mainCalls++
	if p.failFirstOverflowMain && p.mainCalls == 1 {
		return provider.ChatResponse{}, errors.New("context length exceeded")
	}
	return provider.ChatResponse{Content: "done"}, nil
}

func (p *contextTestProvider) Stream(ctx context.Context, req provider.ChatRequest) (<-chan provider.StreamEvent, <-chan error) {
	events := make(chan provider.StreamEvent)
	errs := make(chan error, 1)
	close(events)
	close(errs)
	return events, errs
}

func (p *contextTestProvider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{SupportsTools: true, SupportsStream: true}
}

func isSummaryRequest(req provider.ChatRequest) bool {
	if len(req.Messages) == 0 {
		return false
	}
	if req.Messages[0].Role != "system" {
		return false
	}
	return strings.Contains(req.Messages[0].Content, "compress conversation state")
}

func contextTestConfig(workspace string) config.Config {
	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Agents.Defaults.MaxToolIterations = 2
	cfg.Agents.Defaults.MaxTokens = 128
	cfg.Memory.Enabled = false
	cfg.Skills.Enabled = false
	cfg.ContextControl.Enabled = true
	cfg.ContextControl.MaxHistoryTurns = 12
	cfg.ContextControl.MinHistoryTurns = 4
	cfg.ContextControl.DefaultWindowTokens = 5000
	cfg.ContextControl.OutputReserveTokens = 256
	cfg.ContextControl.Stage1Pct = 1
	cfg.ContextControl.Stage2Pct = 99
	cfg.ContextControl.Stage3Pct = 100
	cfg.ContextControl.Summary.Enabled = true
	cfg.ContextControl.Summary.Method = "model_written"
	cfg.ContextControl.ContextOverflowRetryOnce = true
	cfg.ContextControl.RegistryPath = filepath.Join(workspace, ".squidbot", "model-windows.json")
	return cfg
}

func createEngineForContextTest(t *testing.T, cfg config.Config, p provider.LLMProvider) (*agent.Engine, *storepkg.Store) {
	t.Helper()
	store, err := storepkg.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := agent.NewEngine(cfg, p, "gemma3:4b", store, nil, log.Default())
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	return engine, store
}

func seedHistory(t *testing.T, store *storepkg.Store, sessionID string, turns int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < turns; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		if err := store.AppendTurn(ctx, agent.Turn{
			SessionID: sessionID,
			Role:      role,
			Content:   strings.Repeat("history content ", 8) + time.Now().UTC().Format(time.RFC3339Nano),
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestContextControlStage1ReducesHistoryWithoutSummary(t *testing.T) {
	workspace := t.TempDir()
	cfg := contextTestConfig(workspace)
	cfg.ContextControl.Stage2Pct = 99
	cfg.ContextControl.Summary.Enabled = false

	p := &contextTestProvider{}
	engine, store := createEngineForContextTest(t, cfg, p)
	defer engine.Close()
	defer store.Close()

	sessionID := "cli:stage1"
	seedHistory(t, store, sessionID, 12)
	_, err := engine.Ask(context.Background(), agent.InboundMessage{
		SessionID: sessionID,
		Channel:   "cli",
		ChatID:    "direct",
		SenderID:  "user",
		Content:   "new request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.requests) == 0 {
		t.Fatal("expected at least one provider call")
	}
	req := p.requests[len(p.requests)-1]
	if len(req.Messages) >= 14 {
		t.Fatalf("expected reduced history messages for stage1, got %d", len(req.Messages))
	}
	if _, err := store.GetKV(context.Background(), "context_summary", sessionID); err == nil {
		t.Fatal("did not expect summary persistence for stage1-only flow")
	}
}

func TestContextControlStage2PersistsSummary(t *testing.T) {
	workspace := t.TempDir()
	cfg := contextTestConfig(workspace)
	cfg.ContextControl.Stage2Pct = 2
	cfg.ContextControl.Stage3Pct = 100

	p := &contextTestProvider{}
	engine, store := createEngineForContextTest(t, cfg, p)
	defer engine.Close()
	defer store.Close()

	sessionID := "cli:stage2"
	seedHistory(t, store, sessionID, 12)
	_, err := engine.Ask(context.Background(), agent.InboundMessage{
		SessionID: sessionID,
		Channel:   "cli",
		ChatID:    "direct",
		SenderID:  "user",
		Content:   "summarize long thread",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := store.GetKV(context.Background(), "context_summary", sessionID)
	if err != nil {
		t.Fatalf("expected persisted summary: %v", err)
	}
	if !strings.Contains(string(raw), "summarized") {
		t.Fatalf("expected saved summary content, got %s", string(raw))
	}
	if p.summaryCalls == 0 {
		t.Fatal("expected summary call in stage2 flow")
	}
}

func TestContextControlRestartUsesPersistedSummary(t *testing.T) {
	workspace := t.TempDir()
	cfg := contextTestConfig(workspace)
	cfg.ContextControl.Stage2Pct = 2
	cfg.ContextControl.Stage3Pct = 100
	dbPath := filepath.Join(t.TempDir(), "shared.db")

	store, err := storepkg.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	p1 := &contextTestProvider{}
	engine1, err := agent.NewEngine(cfg, p1, "gemma3:4b", store, nil, log.Default())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "cli:restart"
	seedHistory(t, store, sessionID, 12)
	_, err = engine1.Ask(context.Background(), agent.InboundMessage{
		SessionID: sessionID,
		Channel:   "cli",
		ChatID:    "direct",
		SenderID:  "user",
		Content:   "first pass",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = engine1.Close()

	p2 := &contextTestProvider{}
	engine2, err := agent.NewEngine(cfg, p2, "gemma3:4b", store, nil, log.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer engine2.Close()
	defer store.Close()
	_, err = engine2.Ask(context.Background(), agent.InboundMessage{
		SessionID: sessionID,
		Channel:   "cli",
		ChatID:    "direct",
		SenderID:  "user",
		Content:   "second pass",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(p2.requests) == 0 {
		t.Fatal("expected provider request on restart")
	}
	var systemPrompt string
	for i := len(p2.requests) - 1; i >= 0; i-- {
		if isSummaryRequest(p2.requests[i]) {
			continue
		}
		if len(p2.requests[i].Messages) == 0 {
			continue
		}
		systemPrompt = p2.requests[i].Messages[0].Content
		break
	}
	if strings.TrimSpace(systemPrompt) == "" {
		t.Fatal("expected at least one non-summary request on restart")
	}
	if !strings.Contains(systemPrompt, "## Session Summary") {
		t.Fatalf("expected persisted summary injected after restart, got:\n%s", systemPrompt)
	}
}

func TestContextControlOverflowRetryOnce(t *testing.T) {
	workspace := t.TempDir()
	cfg := contextTestConfig(workspace)
	cfg.ContextControl.Summary.Enabled = false
	cfg.ContextControl.Stage2Pct = 99

	p := &contextTestProvider{failFirstOverflowMain: true}
	engine, store := createEngineForContextTest(t, cfg, p)
	defer engine.Close()
	defer store.Close()

	_, err := engine.Ask(context.Background(), agent.InboundMessage{
		SessionID: "cli:overflow",
		Channel:   "cli",
		ChatID:    "direct",
		SenderID:  "user",
		Content:   "retry context overflow",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.mainCalls != 2 {
		t.Fatalf("expected one retry on overflow, got mainCalls=%d", p.mainCalls)
	}
}

func TestContextControlSummaryFailureFallsBack(t *testing.T) {
	workspace := t.TempDir()
	cfg := contextTestConfig(workspace)
	cfg.ContextControl.Stage2Pct = 2
	cfg.ContextControl.Stage3Pct = 100

	p := &contextTestProvider{failSummary: true}
	engine, store := createEngineForContextTest(t, cfg, p)
	defer engine.Close()
	defer store.Close()

	sessionID := "cli:summary-fallback"
	seedHistory(t, store, sessionID, 12)
	resp, err := engine.Ask(context.Background(), agent.InboundMessage{
		SessionID: sessionID,
		Channel:   "cli",
		ChatID:    "direct",
		SenderID:  "user",
		Content:   "force summary fallback",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(resp) == "" {
		t.Fatal("expected successful response when summary call fails")
	}
	raw, err := store.GetKV(context.Background(), "context_summary", sessionID)
	if err != nil {
		t.Fatalf("expected deterministic fallback summary saved: %v", err)
	}
	if !strings.Contains(string(raw), "Recent compressed history") {
		t.Fatalf("expected deterministic fallback summary, got %s", string(raw))
	}
}
