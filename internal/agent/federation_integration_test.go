package agent_test

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/agent"
	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/federation"
	"github.com/grixate/squidbot/internal/provider"
	storepkg "github.com/grixate/squidbot/internal/storage/bbolt"
	"github.com/grixate/squidbot/internal/telemetry"
)

type federationInstantProvider struct{}

func (p *federationInstantProvider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{}
}

func (p *federationInstantProvider) Stream(ctx context.Context, req provider.ChatRequest) (<-chan provider.StreamEvent, <-chan error) {
	events := make(chan provider.StreamEvent)
	errs := make(chan error, 1)
	close(events)
	close(errs)
	return events, errs
}

func (p *federationInstantProvider) Chat(ctx context.Context, req provider.ChatRequest) (provider.ChatResponse, error) {
	return provider.ChatResponse{Content: "ok"}, nil
}

type federationBlockingProvider struct{}

func (p *federationBlockingProvider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{}
}

func (p *federationBlockingProvider) Stream(ctx context.Context, req provider.ChatRequest) (<-chan provider.StreamEvent, <-chan error) {
	events := make(chan provider.StreamEvent)
	errs := make(chan error, 1)
	close(events)
	close(errs)
	return events, errs
}

func (p *federationBlockingProvider) Chat(ctx context.Context, req provider.ChatRequest) (provider.ChatResponse, error) {
	<-ctx.Done()
	return provider.ChatResponse{}, ctx.Err()
}

func newFederationTestEngine(t *testing.T, providerClient provider.LLMProvider) (*agent.Engine, *storepkg.Store, *telemetry.Metrics) {
	t.Helper()
	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = t.TempDir()
	cfg.Runtime.Subagents.DefaultTimeoutSec = 2
	cfg.Runtime.Subagents.MaxAttempts = 1
	store, err := storepkg.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	metrics := &telemetry.Metrics{}
	engine, err := agent.NewEngine(cfg, providerClient, "test-model", store, metrics, log.New(io.Discard, "", 0))
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = engine.Close()
		_ = store.Close()
	})
	return engine, store, metrics
}

func waitForFederationTerminal(t *testing.T, store *storepkg.Store, runID string, timeout time.Duration) federation.DelegationRun {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		run, err := store.GetFederationRun(context.Background(), runID)
		if err == nil && run.Status.Terminal() {
			return run
		}
		time.Sleep(20 * time.Millisecond)
	}
	run, err := store.GetFederationRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("failed to load run %s: %v", runID, err)
	}
	t.Fatalf("run %s did not reach terminal state: %s", runID, run.Status)
	return federation.DelegationRun{}
}

func TestFederationSubmitIdempotencyReturnsSameRun(t *testing.T) {
	engine, store, metrics := newFederationTestEngine(t, &federationInstantProvider{})
	req := federation.DelegationRequest{
		Task: "summarize session",
		Context: federation.ContextPacket{
			Mode:      "minimal",
			CreatedAt: time.Now().UTC(),
		},
	}
	first, err := engine.FederationSubmit(context.Background(), req, "origin-a", "idem-key-a")
	if err != nil {
		t.Fatalf("first submit failed: %v", err)
	}
	second, err := engine.FederationSubmit(context.Background(), req, "origin-a", "idem-key-a")
	if err != nil {
		t.Fatalf("second submit failed: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected same run id from idempotency dedupe, got %s and %s", first.ID, second.ID)
	}
	if got := metrics.IdempotencyHits.Load(); got == 0 {
		t.Fatalf("expected idempotency hit metric to increment, got %d", got)
	}
	waited := waitForFederationTerminal(t, store, first.ID, 3*time.Second)
	if waited.Status != federation.StatusSucceeded {
		t.Fatalf("expected succeeded run, got %s", waited.Status)
	}
}

func TestFederationCancelTransitionsRun(t *testing.T) {
	engine, store, _ := newFederationTestEngine(t, &federationBlockingProvider{})
	req := federation.DelegationRequest{
		Task: "long running remote request",
		Context: federation.ContextPacket{
			Mode:      "session",
			CreatedAt: time.Now().UTC(),
		},
	}
	run, err := engine.FederationSubmit(context.Background(), req, "origin-b", "idem-key-b")
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, getErr := store.GetFederationRun(context.Background(), run.ID)
		if getErr == nil && current.Status == federation.StatusRunning {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancelled, err := engine.FederationCancel(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("cancel failed: %v", err)
	}
	if cancelled.Status != federation.StatusCancelled {
		t.Fatalf("expected cancelled status from cancel call, got %s", cancelled.Status)
	}
	waited := waitForFederationTerminal(t, store, run.ID, 3*time.Second)
	if waited.Status != federation.StatusCancelled {
		t.Fatalf("expected terminal cancelled status, got %s", waited.Status)
	}
}
