package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/tools"
)

func TestManagerRegistersAndExecutesHTTPTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			resp["result"] = map[string]any{"serverInfo": map[string]any{"name": "fake"}}
		case "tools/list":
			resp["result"] = map[string]any{"tools": []map[string]any{{
				"name":        "echo",
				"description": "Echo text",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"message": map[string]any{"type": "string"}}},
			}}}
		case "tools/call":
			params, _ := req["params"].(map[string]any)
			args, _ := params["arguments"].(map[string]any)
			message, _ := args["message"].(string)
			resp["result"] = map[string]any{"content": []map[string]any{{"type": "text", "text": "echo: " + message}}}
		default:
			resp["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	manager := NewManager(config.MCPToolsConfig{
		Enabled:           true,
		ConnectTimeoutSec: 5,
		Servers: map[string]config.MCPServerConfig{
			"good": {Enabled: true, URL: srv.URL},
		},
	}, t.TempDir(), log.New(io.Discard, "", 0))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := manager.EnsureConnected(ctx); err != nil {
		t.Fatalf("ensure connected failed: %v", err)
	}

	registry := tools.NewRegistry()
	manager.RegisterTools(registry)
	if _, ok := registry.Get("mcp_good_echo"); !ok {
		t.Fatalf("expected wrapped mcp tool, got names: %#v", registry.Names())
	}
	result, err := registry.Execute(context.Background(), "mcp_good_echo", json.RawMessage(`{"message":"hello"}`))
	if err != nil {
		t.Fatalf("tool execution failed: %v", err)
	}
	if !strings.Contains(result.Text, "echo: hello") {
		t.Fatalf("unexpected tool result: %s", result.Text)
	}
}

func TestManagerToleratesPartialConnectionFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		id, hasID := req["id"]
		if !hasID {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		method, _ := req["method"].(string)
		resp := map[string]any{"jsonrpc": "2.0", "id": id}
		if method == "tools/list" {
			resp["result"] = map[string]any{"tools": []map[string]any{{"name": "ok", "inputSchema": map[string]any{"type": "object"}}}}
		} else {
			resp["result"] = map[string]any{}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	manager := NewManager(config.MCPToolsConfig{
		Enabled:           true,
		ConnectTimeoutSec: 2,
		Servers: map[string]config.MCPServerConfig{
			"bad":  {Enabled: true, URL: "http://127.0.0.1:1"},
			"good": {Enabled: true, URL: srv.URL},
		},
	}, t.TempDir(), log.New(io.Discard, "", 0))

	if err := manager.EnsureConnected(context.Background()); err != nil {
		t.Fatalf("expected non-fatal ensure connected, got %v", err)
	}
	registry := tools.NewRegistry()
	manager.RegisterTools(registry)
	if _, ok := registry.Get("mcp_good_ok"); !ok {
		t.Fatalf("expected healthy server tool to be registered, got %#v", registry.Names())
	}
}
