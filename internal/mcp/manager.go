package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/tools"
)

type Manager struct {
	cfg       config.MCPToolsConfig
	workspace string
	log       *log.Logger

	mu          sync.RWMutex
	connecting  bool
	connectDone chan struct{}
	connected   bool
	clients     map[string]rpcClient
	wrapped     []*wrappedMCPTool
}

func NewManager(cfg config.MCPToolsConfig, workspace string, logger *log.Logger) *Manager {
	if logger == nil {
		logger = log.Default()
	}
	if cfg.ConnectTimeoutSec <= 0 {
		cfg.ConnectTimeoutSec = 20
	}
	if cfg.Servers == nil {
		cfg.Servers = map[string]config.MCPServerConfig{}
	}
	return &Manager{
		cfg:       cfg,
		workspace: workspace,
		log:       logger,
		clients:   map[string]rpcClient{},
		wrapped:   []*wrappedMCPTool{},
	}
}

func (m *Manager) EnsureConnected(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if m.connected {
		m.mu.Unlock()
		return nil
	}
	if m.connecting {
		done := m.connectDone
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return nil
		}
	}
	m.connecting = true
	m.connectDone = make(chan struct{})
	done := m.connectDone
	m.mu.Unlock()

	m.connect(ctx)

	m.mu.Lock()
	m.connected = true
	m.connecting = false
	close(done)
	m.mu.Unlock()
	return nil
}

func (m *Manager) connect(ctx context.Context) {
	timeout := time.Duration(m.cfg.ConnectTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	servers := make([]string, 0, len(m.cfg.Servers))
	for name := range m.cfg.Servers {
		servers = append(servers, name)
	}
	sort.Strings(servers)
	usedNames := map[string]struct{}{}
	for _, rawName := range servers {
		serverName := strings.TrimSpace(rawName)
		if serverName == "" {
			continue
		}
		serverCfg := m.cfg.Servers[rawName]
		if !serverCfg.Enabled {
			continue
		}
		connectCtx, cancel := context.WithTimeout(ctx, timeout)
		client, err := m.newClient(connectCtx, serverCfg, timeout)
		cancel()
		if err != nil {
			m.log.Printf("mcp server %q connect failed: %v", serverName, err)
			continue
		}
		listCtx, listCancel := context.WithTimeout(ctx, timeout)
		remoteTools, err := client.ListTools(listCtx)
		listCancel()
		if err != nil {
			m.log.Printf("mcp server %q list tools failed: %v", serverName, err)
			_ = client.Close()
			continue
		}

		m.mu.Lock()
		m.clients[serverName] = client
		for _, remote := range remoteTools {
			name := m.wrapToolName(serverName, remote.Name, serverCfg.ToolPrefix)
			if _, exists := usedNames[name]; exists {
				i := 2
				candidate := fmt.Sprintf("%s_%d", name, i)
				for {
					if _, exists := usedNames[candidate]; !exists {
						name = candidate
						break
					}
					i++
					candidate = fmt.Sprintf("%s_%d", name, i)
				}
			}
			usedNames[name] = struct{}{}
			m.wrapped = append(m.wrapped, &wrappedMCPTool{
				manager:     m,
				name:        name,
				description: strings.TrimSpace(remote.Description),
				schema:      cloneSchema(remote.InputSchema),
				server:      serverName,
				remoteName:  remote.Name,
			})
		}
		m.mu.Unlock()
		m.log.Printf("mcp server %q connected (%d tools)", serverName, len(remoteTools))
	}
}

func (m *Manager) newClient(ctx context.Context, serverCfg config.MCPServerConfig, timeout time.Duration) (rpcClient, error) {
	if strings.TrimSpace(serverCfg.Command) != "" {
		return newStdioRPCClient(ctx, m.workspace, serverCfg, timeout, m.log)
	}
	if strings.TrimSpace(serverCfg.URL) != "" {
		return newHTTPRPCClient(ctx, serverCfg, timeout, m.log)
	}
	return nil, fmt.Errorf("missing command/url")
}

func (m *Manager) RegisterTools(registry *tools.Registry) {
	if m == nil || registry == nil {
		return
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, tool := range m.wrapped {
		registry.Register(tool)
	}
}

func (m *Manager) Call(ctx context.Context, serverName, remoteName string, args map[string]any) (callResult, error) {
	m.mu.RLock()
	client := m.clients[serverName]
	m.mu.RUnlock()
	if client == nil {
		return callResult{}, fmt.Errorf("mcp server %q is not connected", serverName)
	}
	return client.CallTool(ctx, remoteName, args)
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	clients := make([]rpcClient, 0, len(m.clients))
	for _, client := range m.clients {
		clients = append(clients, client)
	}
	m.clients = map[string]rpcClient{}
	m.wrapped = nil
	m.connected = false
	m.mu.Unlock()
	for _, client := range clients {
		_ = client.Close()
	}
	return nil
}

func (m *Manager) wrapToolName(serverName, toolName, overridePrefix string) string {
	prefix := strings.TrimSpace(overridePrefix)
	if prefix == "" {
		prefix = "mcp_" + sanitizeName(serverName)
	}
	prefix = strings.TrimSuffix(sanitizeName(prefix), "_")
	return prefix + "_" + sanitizeName(toolName)
}

type wrappedMCPTool struct {
	manager     *Manager
	name        string
	description string
	schema      map[string]any
	server      string
	remoteName  string
}

func (t *wrappedMCPTool) Name() string { return t.name }

func (t *wrappedMCPTool) Description() string {
	if strings.TrimSpace(t.description) != "" {
		return t.description
	}
	return t.remoteName
}

func (t *wrappedMCPTool) Schema() map[string]any {
	if len(t.schema) > 0 {
		return cloneSchema(t.schema)
	}
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func (t *wrappedMCPTool) Execute(ctx context.Context, args json.RawMessage) (tools.ToolResult, error) {
	arguments := map[string]any{}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &arguments); err != nil {
			return tools.ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
		}
	}
	result, err := t.manager.Call(ctx, t.server, t.remoteName, arguments)
	if err != nil {
		return tools.ToolResult{}, err
	}
	meta := map[string]any{"mcp_server": t.server, "mcp_tool": t.remoteName}
	for key, value := range result.Metadata {
		meta[key] = value
	}
	return tools.ToolResult{Text: result.Text, Metadata: meta}, nil
}

func sanitizeName(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "tool"
	}
	var b strings.Builder
	for _, r := range raw {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		case r == '_':
			b.WriteRune(r)
		case r == '-':
			b.WriteRune('_')
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(strings.TrimSpace(b.String()), "_")
	if out == "" {
		return "tool"
	}
	return strings.ToLower(out)
}

func cloneSchema(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
