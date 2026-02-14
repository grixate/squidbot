package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/agent"
	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/provider"
	storepkg "github.com/grixate/squidbot/internal/storage/bbolt"
	"github.com/grixate/squidbot/internal/subagent"
)

type scriptedParentProvider struct {
	mu            sync.Mutex
	parentCalls   int
	subagentCalls int
	mode          string
	subDelay      time.Duration
	spawnCount    int
	cancelRunID   string
}

func (p *scriptedParentProvider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{SupportsTools: true}
}

func (p *scriptedParentProvider) Stream(ctx context.Context, req provider.ChatRequest) (<-chan provider.StreamEvent, <-chan error) {
	events := make(chan provider.StreamEvent)
	errs := make(chan error, 1)
	close(events)
	close(errs)
	return events, errs
}

func (p *scriptedParentProvider) Chat(ctx context.Context, req provider.ChatRequest) (provider.ChatResponse, error) {
	if isSubagentRequest(req.Messages) {
		select {
		case <-ctx.Done():
			return provider.ChatResponse{}, ctx.Err()
		case <-time.After(p.subDelay):
		}
		p.mu.Lock()
		call := p.subagentCalls
		p.subagentCalls++
		p.mu.Unlock()
		if p.mode == "write_block" && call%2 == 0 {
			args, _ := json.Marshal(map[string]any{"path": "proof.txt", "content": "blocked"})
			return provider.ChatResponse{ToolCalls: []provider.ToolCall{{ID: "w1", Name: "write_file", Arguments: args}}}, nil
		}
		return provider.ChatResponse{Content: "subagent finished"}, nil
	}

	p.mu.Lock()
	call := p.parentCalls
	p.parentCalls++
	p.mu.Unlock()

	switch p.mode {
	case "attachment_outside":
		if call == 0 {
			args, _ := json.Marshal(map[string]any{"task": "bad attach", "attachments": []string{"../outside.txt"}, "wait": true})
			return provider.ChatResponse{ToolCalls: []provider.ToolCall{{ID: "s1", Name: "spawn", Arguments: args}}}, nil
		}
		return provider.ChatResponse{Content: "done"}, nil
	case "write_block":
		if call == 0 {
			args, _ := json.Marshal(map[string]any{"task": "attempt write", "wait": true})
			return provider.ChatResponse{ToolCalls: []provider.ToolCall{{ID: "s1", Name: "spawn", Arguments: args}}}, nil
		}
		return provider.ChatResponse{Content: "done"}, nil
	case "cancel_isolation":
		if call == 0 {
			args, _ := json.Marshal(map[string]any{"run_id": p.cancelRunID})
			return provider.ChatResponse{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "subagent_cancel", Arguments: args}}}, nil
		}
		return provider.ChatResponse{Content: "done"}, nil
	case "mailbox_pressure":
		if call == 0 {
			calls := make([]provider.ToolCall, 0, p.spawnCount)
			for i := 0; i < p.spawnCount; i++ {
				args, _ := json.Marshal(map[string]any{"task": fmt.Sprintf("pressure-%d", i), "wait": false})
				calls = append(calls, provider.ToolCall{ID: fmt.Sprintf("sp-%d", i), Name: "spawn", Arguments: args})
			}
			return provider.ChatResponse{ToolCalls: calls}, nil
		}
		return provider.ChatResponse{Content: "queued"}, nil
	default:
		return provider.ChatResponse{Content: "ok"}, nil
	}
}

func TestSubagentSecurityRejectsAttachmentOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Runtime.Subagents.NotifyOnComplete = false

	store, err := storepkg.Open(filepath.Join(t.TempDir(), "sec1.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	providerStub := &scriptedParentProvider{mode: "attachment_outside"}
	engine, err := agent.NewEngine(cfg, providerStub, "test-model", store, nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	_, err = engine.Ask(context.Background(), agent.InboundMessage{SessionID: "cli:sec1", Channel: "cli", ChatID: "direct", SenderID: "user", Content: "spawn", CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	runs, err := store.ListSubagentRunsBySession(context.Background(), "cli:sec1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Fatalf("expected no runs due to attachment rejection, got %d", len(runs))
	}
}

func TestSubagentSecurityDisablesWriteToolsAndKeepsArtifactsInTree(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Runtime.Subagents.AllowWrites = false
	cfg.Runtime.Subagents.NotifyOnComplete = false

	store, err := storepkg.Open(filepath.Join(t.TempDir(), "sec2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	providerStub := &scriptedParentProvider{mode: "write_block", subDelay: 20 * time.Millisecond}
	engine, err := agent.NewEngine(cfg, providerStub, "test-model", store, nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	_, err = engine.Ask(context.Background(), agent.InboundMessage{SessionID: "cli:sec2", Channel: "cli", ChatID: "direct", SenderID: "user", Content: "spawn", CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, "proof.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("expected proof.txt to not be created, statErr=%v", statErr)
	}
	runs, err := store.ListSubagentRunsBySession(context.Background(), "cli:sec2", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].Result == nil || len(runs[0].Result.ArtifactPaths) == 0 {
		t.Fatalf("expected artifacts for run: %+v", runs[0])
	}
	prefix := filepath.Join(workspace, ".squidbot", "subagents") + string(filepath.Separator)
	for _, artifact := range runs[0].Result.ArtifactPaths {
		if !strings.HasPrefix(artifact, prefix) {
			t.Fatalf("artifact outside subagent tree: %s", artifact)
		}
	}
}

func TestSubagentCancelIsolationBySession(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Runtime.Subagents.NotifyOnComplete = false

	store, err := storepkg.Open(filepath.Join(t.TempDir(), "sec3.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	run := subagent.Run{ID: "foreign-run", SessionID: "cli:owner", Task: "keep", Status: subagent.StatusQueued, CreatedAt: time.Now().UTC(), TimeoutSec: 30, MaxAttempts: 1}
	finished := time.Now().UTC()
	run.Status = subagent.StatusCancelled
	run.Error = "already done"
	run.FinishedAt = &finished
	if err := store.PutSubagentRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}

	providerStub := &scriptedParentProvider{mode: "cancel_isolation", cancelRunID: "foreign-run"}
	engine, err := agent.NewEngine(cfg, providerStub, "test-model", store, nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	_, err = engine.Ask(context.Background(), agent.InboundMessage{SessionID: "cli:other", Channel: "cli", ChatID: "direct", SenderID: "user", Content: "cancel", CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.GetSubagentRun(context.Background(), "foreign-run")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != subagent.StatusCancelled {
		t.Fatalf("expected cancelled status to remain unchanged, got %s", updated.Status)
	}
}

func TestSubagentRunsReachTerminalUnderMailboxPressure(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Runtime.MailboxSize = 1
	cfg.Runtime.Subagents.ReinjectCompletion = true
	cfg.Runtime.Subagents.NotifyOnComplete = true
	cfg.Runtime.Subagents.MaxConcurrent = 6
	cfg.Runtime.Subagents.MaxQueue = 128

	store, err := storepkg.Open(filepath.Join(t.TempDir(), "pressure.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	providerStub := &scriptedParentProvider{mode: "mailbox_pressure", spawnCount: 40, subDelay: 25 * time.Millisecond}
	engine, err := agent.NewEngine(cfg, providerStub, "test-model", store, nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	_, err = engine.Ask(context.Background(), agent.InboundMessage{SessionID: "cli:pressure", Channel: "cli", ChatID: "direct", SenderID: "user", Content: "pressure", CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for {
		runs, err := store.ListSubagentRunsBySession(context.Background(), "cli:pressure", 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) == 40 {
			allTerminal := true
			for _, run := range runs {
				if !run.Status.Terminal() {
					allTerminal = false
					break
				}
			}
			if allTerminal {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for terminal runs under pressure")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
