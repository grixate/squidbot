package agent_test

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/agent"
	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/provider"
	storepkg "github.com/grixate/squidbot/internal/storage/bbolt"
)

type fakeProvider struct {
	calls int
}

func (f *fakeProvider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{SupportsTools: true}
}

func (f *fakeProvider) Stream(ctx context.Context, req provider.ChatRequest) (<-chan provider.StreamEvent, <-chan error) {
	events := make(chan provider.StreamEvent)
	errs := make(chan error, 1)
	close(events)
	close(errs)
	return events, errs
}

func (f *fakeProvider) Chat(ctx context.Context, req provider.ChatRequest) (provider.ChatResponse, error) {
	f.calls++
	if f.calls == 1 {
		args, _ := json.Marshal(map[string]string{"path": "."})
		return provider.ChatResponse{ToolCalls: []provider.ToolCall{{ID: "1", Name: "list_dir", Arguments: args}}}, nil
	}
	return provider.ChatResponse{Content: "done"}, nil
}

func TestEngineToolLoop(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Agents.Defaults.MaxToolIterations = 5
	cfg.Agents.Defaults.ToolTimeoutSec = 5

	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := storepkg.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	engine, err := agent.NewEngine(cfg, &fakeProvider{}, "test-model", store, nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	resp, err := engine.Ask(context.Background(), agent.InboundMessage{
		SessionID: "cli:test",
		Channel:   "cli",
		ChatID:    "direct",
		SenderID:  "user",
		Content:   "hello",
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp != "done" {
		t.Fatalf("unexpected response: %s", resp)
	}
}
