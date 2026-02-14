package plugins

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/grixate/squidbot/internal/config"
)

type Manager struct {
	enabled bool
	paths   []string
	logger  *log.Logger

	mu           sync.RWMutex
	manifests    map[string]discoveredManifest
	toolIndex    map[string]RegisteredTool
	toolToPlugin map[string]string
	clients      map[string]*processClient
}

func NewManager(cfg config.Config, logger *log.Logger) *Manager {
	if logger == nil {
		logger = log.Default()
	}
	paths := append([]string(nil), cfg.Runtime.Plugins.Paths...)
	if len(paths) == 0 {
		paths = []string{filepath.Join(config.WorkspacePath(cfg), "plugins")}
	}
	return &Manager{
		enabled:      cfg.Features.Plugins || cfg.Runtime.Plugins.Enabled,
		paths:        paths,
		logger:       logger,
		manifests:    map[string]discoveredManifest{},
		toolIndex:    map[string]RegisteredTool{},
		toolToPlugin: map[string]string{},
		clients:      map[string]*processClient{},
	}
}

func (m *Manager) Discover(ctx context.Context) error {
	_ = ctx
	if m == nil || !m.enabled {
		return nil
	}
	manifests, err := discoverManifests(m.paths)
	if err != nil {
		return err
	}
	loaded := map[string]discoveredManifest{}
	toolIndex := map[string]RegisteredTool{}
	toolToPlugin := map[string]string{}
	for _, manifest := range manifests {
		pluginName := strings.TrimSpace(manifest.Name)
		loaded[pluginName] = manifest
		for _, tool := range manifest.Tools {
			namespaced := fmt.Sprintf("plugin.%s.%s", pluginName, strings.TrimSpace(tool.Name))
			toolIndex[namespaced] = RegisteredTool{
				NamespacedName: namespaced,
				PluginName:     pluginName,
				Name:           strings.TrimSpace(tool.Name),
				Description:    strings.TrimSpace(tool.Description),
				Schema:         tool.Schema,
			}
			toolToPlugin[namespaced] = pluginName
		}
	}
	m.mu.Lock()
	m.manifests = loaded
	m.toolIndex = toolIndex
	m.toolToPlugin = toolToPlugin
	m.mu.Unlock()
	return nil
}

func (m *Manager) Tools() []RegisteredTool {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]RegisteredTool, 0, len(m.toolIndex))
	for _, tool := range m.toolIndex {
		out = append(out, tool)
	}
	return out
}

func (m *Manager) Call(ctx context.Context, namespacedName string, args json.RawMessage) (CallResult, error) {
	if m == nil || !m.enabled {
		return CallResult{}, fmt.Errorf("plugins disabled")
	}
	m.mu.RLock()
	tool, ok := m.toolIndex[namespacedName]
	pluginName := m.toolToPlugin[namespacedName]
	manifest := m.manifests[pluginName]
	m.mu.RUnlock()
	if !ok {
		return CallResult{}, fmt.Errorf("plugin tool not found: %s", namespacedName)
	}
	client, err := m.clientFor(pluginName, manifest)
	if err != nil {
		return CallResult{}, err
	}
	payload := map[string]any{
		"tool":      tool.Name,
		"arguments": json.RawMessage(args),
	}
	result, err := client.call(ctx, "tool.call", payload)
	if err != nil {
		return CallResult{}, err
	}
	var out CallResult
	if err := json.Unmarshal(result, &out); err != nil {
		return CallResult{}, fmt.Errorf("plugin %s returned invalid response: %w", pluginName, err)
	}
	return out, nil
}

func (m *Manager) clientFor(pluginName string, manifest discoveredManifest) (*processClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.clients[pluginName]; ok {
		return existing, nil
	}
	client, err := startProcessClient(manifest, m.logger)
	if err != nil {
		return nil, err
	}
	m.clients[pluginName] = client
	return client, nil
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, client := range m.clients {
		if err := client.close(); err != nil {
			m.logger.Printf("plugin %s close error: %v", name, err)
		}
	}
	m.clients = map[string]*processClient{}
	return nil
}

type processClient struct {
	manifest discoveredManifest
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   *bufio.Reader
	mu       sync.Mutex
	seq      atomic.Int64
}

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

func startProcessClient(manifest discoveredManifest, logger *log.Logger) (*processClient, error) {
	args := append([]string(nil), manifest.Args...)
	cmd := exec.Command(strings.TrimSpace(manifest.Command), args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	if logger != nil {
		logger.Printf("plugin started name=%s path=%s", manifest.Name, manifest.Path)
	}
	client := &processClient{manifest: manifest, cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdoutPipe)}
	return client, nil
}

func (c *processClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.seq.Add(1)
	req := jsonRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return nil, err
	}
	type responseResult struct {
		resp jsonRPCResponse
		err  error
	}
	respCh := make(chan responseResult, 1)
	go func() {
		line, readErr := c.stdout.ReadBytes('\n')
		if readErr != nil {
			respCh <- responseResult{err: readErr}
			return
		}
		var resp jsonRPCResponse
		if unmarshalErr := json.Unmarshal(line, &resp); unmarshalErr != nil {
			respCh <- responseResult{err: unmarshalErr}
			return
		}
		respCh <- responseResult{resp: resp}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case outcome := <-respCh:
		if outcome.err != nil {
			return nil, outcome.err
		}
		if outcome.resp.Error != nil {
			return nil, fmt.Errorf("plugin rpc error %d: %s", outcome.resp.Error.Code, outcome.resp.Error.Message)
		}
		return outcome.resp.Result, nil
	}
}

func (c *processClient) close() error {
	if c == nil {
		return nil
	}
	_ = c.stdin.Close()
	if c.cmd.Process == nil {
		return nil
	}
	_ = c.cmd.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()
	select {
	case <-time.After(2 * time.Second):
		_ = c.cmd.Process.Kill()
		return <-done
	case err := <-done:
		return err
	}
}
