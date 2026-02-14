package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/agent"
	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/provider"
	storepkg "github.com/grixate/squidbot/internal/storage/bbolt"
)

type fanoutProvider struct {
	mu            sync.Mutex
	parentCalls   int
	subagentCalls int
	delay         time.Duration
}

func (p *fanoutProvider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{SupportsTools: true}
}

func (p *fanoutProvider) Stream(ctx context.Context, req provider.ChatRequest) (<-chan provider.StreamEvent, <-chan error) {
	events := make(chan provider.StreamEvent)
	errs := make(chan error, 1)
	close(events)
	close(errs)
	return events, errs
}

func (p *fanoutProvider) Chat(ctx context.Context, req provider.ChatRequest) (provider.ChatResponse, error) {
	if isSubagentRequest(req.Messages) {
		select {
		case <-ctx.Done():
			return provider.ChatResponse{}, ctx.Err()
		case <-time.After(p.delay):
		}
		task := lastUserMessage(req.Messages)
		p.mu.Lock()
		p.subagentCalls++
		p.mu.Unlock()
		return provider.ChatResponse{Content: "subagent done: " + task}, nil
	}

	p.mu.Lock()
	call := p.parentCalls
	p.parentCalls++
	p.mu.Unlock()

	switch call {
	case 0:
		calls := make([]provider.ToolCall, 0, 8)
		for i := 0; i < 8; i++ {
			args, _ := json.Marshal(map[string]any{
				"task":  fmt.Sprintf("task-%d", i),
				"label": fmt.Sprintf("label-%d", i),
				"wait":  false,
			})
			calls = append(calls, provider.ToolCall{ID: fmt.Sprintf("spawn-%d", i), Name: "spawn", Arguments: args})
		}
		return provider.ChatResponse{ToolCalls: calls}, nil
	case 1:
		runIDs := extractRunIDs(req.Messages)
		if len(runIDs) != 8 {
			return provider.ChatResponse{}, fmt.Errorf("expected 8 run IDs, got %d", len(runIDs))
		}
		args, _ := json.Marshal(map[string]any{"run_ids": runIDs, "timeout_sec": 20})
		return provider.ChatResponse{ToolCalls: []provider.ToolCall{{ID: "wait-1", Name: "subagent_wait", Arguments: args}}}, nil
	default:
		return provider.ChatResponse{Content: "fanout complete"}, nil
	}
}

func isSubagentRequest(messages []provider.Message) bool {
	for _, msg := range messages {
		if msg.Role != "system" {
			continue
		}
		if strings.Contains(strings.ToLower(msg.Content), "background subagent") {
			return true
		}
	}
	return false
}

func lastUserMessage(messages []provider.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

func extractRunIDs(messages []provider.Message) []string {
	re := regexp.MustCompile(`run_id:\s*([A-Za-z0-9_-]+)`)
	seen := map[string]struct{}{}
	ids := make([]string, 0, 8)
	for _, msg := range messages {
		if msg.Role != "tool" {
			continue
		}
		matches := re.FindAllStringSubmatch(msg.Content, -1)
		for _, m := range matches {
			if len(m) < 2 {
				continue
			}
			id := strings.TrimSpace(m[1])
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func TestEngineFanOutFanInParallelSubagents(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Agents.Defaults.MaxToolIterations = 10
	cfg.Agents.Defaults.ToolTimeoutSec = 20
	cfg.Runtime.Subagents.MaxConcurrent = 4
	cfg.Runtime.Subagents.MaxQueue = 64
	cfg.Runtime.Subagents.DefaultTimeoutSec = 20
	cfg.Runtime.Subagents.MaxAttempts = 1
	cfg.Runtime.Subagents.NotifyOnComplete = false

	dbPath := filepath.Join(t.TempDir(), "fanout.db")
	store, err := storepkg.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	providerStub := &fanoutProvider{delay: 200 * time.Millisecond}
	engine, err := agent.NewEngine(cfg, providerStub, "test-model", store, nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := engine.Ask(ctx, agent.InboundMessage{
		SessionID: "cli:fanout",
		Channel:   "cli",
		ChatID:    "direct",
		SenderID:  "user",
		Content:   "Run fanout with 8 subtasks and wait for all.",
		CreatedAt: time.Now().UTC(),
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if resp != "fanout complete" {
		t.Fatalf("unexpected response: %q", resp)
	}
	providerStub.mu.Lock()
	subCalls := providerStub.subagentCalls
	providerStub.mu.Unlock()
	if subCalls != 8 {
		t.Fatalf("expected 8 subagent calls, got %d", subCalls)
	}
	parallelBatches := (8 + cfg.Runtime.Subagents.MaxConcurrent - 1) / cfg.Runtime.Subagents.MaxConcurrent
	expectedParallel := time.Duration(parallelBatches) * providerStub.delay
	maxAllowed := expectedParallel*3 + 300*time.Millisecond
	if elapsed > maxAllowed {
		t.Fatalf("fanout took too long (%s), expected <= %s for parallel execution", elapsed, maxAllowed)
	}
}
