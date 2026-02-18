package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/agent"
	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/provider"
	storepkg "github.com/grixate/squidbot/internal/storage/bbolt"
)

type parentMCPProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *parentMCPProvider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{SupportsTools: true}
}

func (p *parentMCPProvider) Stream(ctx context.Context, req provider.ChatRequest) (<-chan provider.StreamEvent, <-chan error) {
	events := make(chan provider.StreamEvent)
	errs := make(chan error, 1)
	close(events)
	close(errs)
	return events, errs
}

func (p *parentMCPProvider) Chat(ctx context.Context, req provider.ChatRequest) (provider.ChatResponse, error) {
	p.mu.Lock()
	call := p.calls
	p.calls++
	p.mu.Unlock()
	switch call {
	case 0:
		args, _ := json.Marshal(map[string]any{"message": "from-parent"})
		return provider.ChatResponse{ToolCalls: []provider.ToolCall{{ID: "mcp-1", Name: "mcp_test_echo", Arguments: args}}}, nil
	case 1:
		if !hasToolOutput(req.Messages, "echo: from-parent") {
			return provider.ChatResponse{}, fmt.Errorf("expected parent MCP tool output in history")
		}
		return provider.ChatResponse{Content: "parent mcp ok"}, nil
	default:
		return provider.ChatResponse{Content: "done"}, nil
	}
}

type spawnMCPProvider struct {
	mu          sync.Mutex
	parentCalls int
	subCalls    int
}

func (p *spawnMCPProvider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{SupportsTools: true}
}

func (p *spawnMCPProvider) Stream(ctx context.Context, req provider.ChatRequest) (<-chan provider.StreamEvent, <-chan error) {
	events := make(chan provider.StreamEvent)
	errs := make(chan error, 1)
	close(events)
	close(errs)
	return events, errs
}

func (p *spawnMCPProvider) Chat(ctx context.Context, req provider.ChatRequest) (provider.ChatResponse, error) {
	if isSubagentRequestMCP(req.Messages) {
		p.mu.Lock()
		call := p.subCalls
		p.subCalls++
		p.mu.Unlock()
		switch call {
		case 0:
			args, _ := json.Marshal(map[string]any{"message": "from-sub"})
			return provider.ChatResponse{ToolCalls: []provider.ToolCall{{ID: "sub-mcp-1", Name: "mcp_test_echo", Arguments: args}}}, nil
		case 1:
			if !hasToolOutput(req.Messages, "echo: from-sub") {
				return provider.ChatResponse{}, fmt.Errorf("expected subagent MCP output in history")
			}
			return provider.ChatResponse{Content: "subagent mcp ok"}, nil
		default:
			return provider.ChatResponse{Content: "subagent done"}, nil
		}
	}

	p.mu.Lock()
	call := p.parentCalls
	p.parentCalls++
	p.mu.Unlock()
	switch call {
	case 0:
		args, _ := json.Marshal(map[string]any{"task": "use mcp in subagent", "wait": true, "timeout_sec": 15})
		return provider.ChatResponse{ToolCalls: []provider.ToolCall{{ID: "spawn-1", Name: "spawn", Arguments: args}}}, nil
	case 1:
		return provider.ChatResponse{Content: "parent spawn ok"}, nil
	default:
		return provider.ChatResponse{Content: "done"}, nil
	}
}

func TestEngineParentLoopUsesMCPTools(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Features.MCP = true
	cfg.Tools.MCP.Enabled = true
	cfg.Tools.MCP.Servers = map[string]config.MCPServerConfig{}

	srv := newMCPTestServer()
	defer srv.Close()
	cfg.Tools.MCP.Servers["test"] = config.MCPServerConfig{Enabled: true, URL: srv.URL}

	store, err := storepkg.Open(filepath.Join(t.TempDir(), "mcp-parent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	engine, err := agent.NewEngine(cfg, &parentMCPProvider{}, "test-model", store, nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	resp, err := engine.Ask(context.Background(), agent.InboundMessage{
		SessionID: "cli:mcp-parent",
		Channel:   "cli",
		ChatID:    "direct",
		SenderID:  "user",
		Content:   "run mcp",
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp != "parent mcp ok" {
		t.Fatalf("unexpected response: %q", resp)
	}
	events, err := store.ListToolEvents(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.ToolName == "mcp_test_echo" && strings.Contains(event.Output, "echo: from-parent") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected mcp_test_echo tool event with MCP output, got %#v", events)
	}
}

func TestEngineSubagentLoopUsesMCPTools(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Features.MCP = true
	cfg.Tools.MCP.Enabled = true
	cfg.Tools.MCP.Servers = map[string]config.MCPServerConfig{}
	cfg.Runtime.Subagents.NotifyOnComplete = false
	srv := newMCPTestServer()
	defer srv.Close()
	cfg.Tools.MCP.Servers["test"] = config.MCPServerConfig{Enabled: true, URL: srv.URL}

	store, err := storepkg.Open(filepath.Join(t.TempDir(), "mcp-subagent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	engine, err := agent.NewEngine(cfg, &spawnMCPProvider{}, "test-model", store, nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	resp, err := engine.Ask(context.Background(), agent.InboundMessage{
		SessionID: "cli:mcp-sub",
		Channel:   "cli",
		ChatID:    "direct",
		SenderID:  "user",
		Content:   "spawn with mcp",
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp, "parent spawn ok") {
		t.Fatalf("unexpected parent response: %q", resp)
	}
	runs, err := store.ListSubagentRunsBySession(context.Background(), "cli:mcp-sub", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected one subagent run, got %d", len(runs))
	}
	if runs[0].Result == nil || !strings.Contains(runs[0].Result.Output, "subagent mcp ok") {
		t.Fatalf("expected subagent MCP result, got %+v", runs[0].Result)
	}
}

func hasToolOutput(messages []provider.Message, needle string) bool {
	needle = strings.ToLower(strings.TrimSpace(needle))
	for _, msg := range messages {
		if msg.Role != "tool" {
			continue
		}
		if strings.Contains(strings.ToLower(msg.Content), needle) {
			return true
		}
	}
	return false
}

func isSubagentRequestMCP(messages []provider.Message) bool {
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

func newMCPTestServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		id, hasID := req["id"]
		method, _ := req["method"].(string)
		if !hasID {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		resp := map[string]any{"jsonrpc": "2.0", "id": id}
		switch method {
		case "initialize":
			resp["result"] = map[string]any{"serverInfo": map[string]any{"name": "test-mcp"}}
		case "tools/list":
			resp["result"] = map[string]any{"tools": []map[string]any{{
				"name":        "echo",
				"description": "Echo text",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"message": map[string]any{"type": "string"}}},
			}}}
		case "tools/call":
			params, _ := req["params"].(map[string]any)
			arguments, _ := params["arguments"].(map[string]any)
			message, _ := arguments["message"].(string)
			resp["result"] = map[string]any{"content": []map[string]any{{"type": "text", "text": "echo: " + message}}}
		default:
			resp["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}
