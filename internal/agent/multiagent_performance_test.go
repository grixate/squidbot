package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/agent"
	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/provider"
	storepkg "github.com/grixate/squidbot/internal/storage/bbolt"
)

type perfFanoutProvider struct {
	mu          sync.Mutex
	parentCalls int
	taskCount   int
	subDelay    time.Duration
}

func (p *perfFanoutProvider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{SupportsTools: true}
}

func (p *perfFanoutProvider) Stream(ctx context.Context, req provider.ChatRequest) (<-chan provider.StreamEvent, <-chan error) {
	events := make(chan provider.StreamEvent)
	errs := make(chan error, 1)
	close(events)
	close(errs)
	return events, errs
}

func (p *perfFanoutProvider) Chat(ctx context.Context, req provider.ChatRequest) (provider.ChatResponse, error) {
	if isSubagentRequest(req.Messages) {
		select {
		case <-ctx.Done():
			return provider.ChatResponse{}, ctx.Err()
		case <-time.After(p.subDelay):
		}
		return provider.ChatResponse{Content: "ok"}, nil
	}

	p.mu.Lock()
	call := p.parentCalls
	p.parentCalls++
	p.mu.Unlock()

	switch call {
	case 0:
		calls := make([]provider.ToolCall, 0, p.taskCount)
		for i := 0; i < p.taskCount; i++ {
			args, _ := json.Marshal(map[string]any{"task": fmt.Sprintf("perf-%d", i), "wait": false})
			calls = append(calls, provider.ToolCall{ID: fmt.Sprintf("sp-%d", i), Name: "spawn", Arguments: args})
		}
		return provider.ChatResponse{ToolCalls: calls}, nil
	case 1:
		runIDs := extractRunIDs(req.Messages)
		args, _ := json.Marshal(map[string]any{"run_ids": runIDs, "timeout_sec": int((time.Duration(p.taskCount)+2)*p.subDelay/time.Second) + 30})
		return provider.ChatResponse{ToolCalls: []provider.ToolCall{{ID: "wait", Name: "subagent_wait", Arguments: args}}}, nil
	default:
		return provider.ChatResponse{Content: "perf complete"}, nil
	}
}

func runPerfScenario(t *testing.T, maxConcurrent int, taskCount int, subDelay time.Duration) time.Duration {
	t.Helper()
	workspace := t.TempDir()
	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Agents.Defaults.MaxToolIterations = 10
	cfg.Agents.Defaults.ToolTimeoutSec = int(subDelay.Seconds()) + 10
	cfg.Runtime.Subagents.MaxConcurrent = maxConcurrent
	cfg.Runtime.Subagents.MaxQueue = 256
	cfg.Runtime.Subagents.DefaultTimeoutSec = int(subDelay.Seconds()) + 10
	cfg.Runtime.Subagents.MaxAttempts = 1
	cfg.Runtime.Subagents.NotifyOnComplete = false

	store, err := storepkg.Open(filepath.Join(t.TempDir(), fmt.Sprintf("perf-%d.db", maxConcurrent)))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	providerStub := &perfFanoutProvider{taskCount: taskCount, subDelay: subDelay}
	engine, err := agent.NewEngine(cfg, providerStub, "test-model", store, nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(taskCount+10)*subDelay)
	defer cancel()
	resp, err := engine.Ask(ctx, agent.InboundMessage{SessionID: fmt.Sprintf("cli:perf:%d", maxConcurrent), Channel: "cli", ChatID: "direct", SenderID: "user", Content: "run perf", CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if resp != "perf complete" {
		t.Fatalf("unexpected response: %q", resp)
	}
	return time.Since(start)
}

func TestEnginePerformanceTarget12x2s(t *testing.T) {
	if os.Getenv("SQUIDBOT_RUN_PERF_TESTS") != "1" {
		t.Skip("set SQUIDBOT_RUN_PERF_TESTS=1 to run long performance target tests")
	}
	const taskCount = 12
	const delay = 2 * time.Second

	parallel := runPerfScenario(t, 4, taskCount, delay)
	if parallel > 7*time.Second {
		t.Fatalf("parallel run exceeded target: %s > 7s", parallel)
	}
	sequential := runPerfScenario(t, 1, taskCount, delay)
	if sequential <= parallel {
		t.Fatalf("sequential baseline should be slower: seq=%s parallel=%s", sequential, parallel)
	}
	reduction := float64(sequential-parallel) / float64(sequential)
	if reduction < 0.35 {
		t.Fatalf("expected at least 35%% latency reduction, got %.2f%% (seq=%s parallel=%s)", reduction*100, sequential, parallel)
	}
}
