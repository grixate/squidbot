package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/grixate/squidbot/internal/config"
)

type remoteTool struct {
	Name        string
	Description string
	InputSchema map[string]any
}

type callResult struct {
	Text     string
	Metadata map[string]any
}

type rpcClient interface {
	ListTools(ctx context.Context) ([]remoteTool, error)
	CallTool(ctx context.Context, name string, args map[string]any) (callResult, error)
	Close() error
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error,omitempty"`
}

type stdioRPCClient struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	logger   *log.Logger
	writeMu  sync.Mutex
	readDone chan struct{}
	nextID   int64

	pendingMu sync.Mutex
	pending   map[int64]chan rpcResponse
	closed    atomic.Bool
}

func newStdioRPCClient(ctx context.Context, workspace string, cfg config.MCPServerConfig, timeout time.Duration, logger *log.Logger) (*stdioRPCClient, error) {
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	if strings.TrimSpace(workspace) != "" {
		cmd.Dir = workspace
	}
	cmd.Env = os.Environ()
	for key, value := range cfg.Env {
		k := strings.TrimSpace(key)
		v := strings.TrimSpace(value)
		if k == "" {
			continue
		}
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	client := &stdioRPCClient{
		cmd:      cmd,
		stdin:    stdin,
		stdout:   stdout,
		logger:   logger,
		readDone: make(chan struct{}),
		pending:  map[int64]chan rpcResponse{},
	}
	go client.readLoop()

	initCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := client.initialize(initCtx); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func (c *stdioRPCClient) initialize(ctx context.Context) error {
	_, err := c.request(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "squidbot",
			"version": "dev",
		},
	})
	if err != nil {
		return err
	}
	return c.notify(ctx, "notifications/initialized", map[string]any{})
}

func (c *stdioRPCClient) ListTools(ctx context.Context) ([]remoteTool, error) {
	result, err := c.request(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	return parseToolsList(result)
}

func (c *stdioRPCClient) CallTool(ctx context.Context, name string, args map[string]any) (callResult, error) {
	result, err := c.request(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return callResult{}, err
	}
	return parseCallResult(result)
}

func (c *stdioRPCClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := atomic.AddInt64(&c.nextID, 1)
	respCh := make(chan rpcResponse, 1)
	c.pendingMu.Lock()
	c.pending[id] = respCh
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	req := rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if err := c.writeFrame(payload); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-respCh:
		if resp.Error != nil {
			return nil, fmt.Errorf("mcp rpc %s failed: %s", method, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

func (c *stdioRPCClient) notify(ctx context.Context, method string, params any) error {
	req := rpcRequest{JSONRPC: "2.0", Method: method, Params: params}
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return c.writeFrame(payload)
}

func (c *stdioRPCClient) writeFrame(payload []byte) error {
	if c.closed.Load() {
		return fmt.Errorf("mcp stdio client is closed")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(payload))
	if _, err := io.WriteString(c.stdin, header); err != nil {
		return err
	}
	_, err := c.stdin.Write(payload)
	return err
}

func (c *stdioRPCClient) readLoop() {
	defer close(c.readDone)
	reader := bufio.NewReader(c.stdout)
	for {
		payload, err := readRPCFrame(reader)
		if err != nil {
			if !errors.Is(err, io.EOF) && c.logger != nil {
				c.logger.Printf("mcp stdio read failed: %v", err)
			}
			c.failAllPending(err)
			return
		}
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(payload, &probe); err != nil {
			continue
		}
		idRaw, hasID := probe["id"]
		if !hasID {
			continue
		}
		var id int64
		if err := json.Unmarshal(idRaw, &id); err != nil {
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal(payload, &resp); err != nil {
			continue
		}
		c.pendingMu.Lock()
		ch := c.pending[id]
		c.pendingMu.Unlock()
		if ch != nil {
			ch <- resp
		}
	}
}

func (c *stdioRPCClient) failAllPending(err error) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for id, ch := range c.pending {
		if ch == nil {
			continue
		}
		resp := rpcResponse{ID: id, Error: &rpcError{Code: -1, Message: defaultErrMsg(err)}}
		select {
		case ch <- resp:
		default:
		}
	}
}

func (c *stdioRPCClient) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	_ = c.stdin.Close()
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	<-c.readDone
	if c.cmd != nil {
		_ = c.cmd.Wait()
	}
	return nil
}

type httpRPCClient struct {
	url     string
	headers map[string]string
	client  *http.Client
	logger  *log.Logger
	nextID  int64
}

func newHTTPRPCClient(ctx context.Context, cfg config.MCPServerConfig, timeout time.Duration, logger *log.Logger) (*httpRPCClient, error) {
	client := &httpRPCClient{
		url:     strings.TrimSpace(cfg.URL),
		headers: cloneHeaders(cfg.Headers),
		client:  &http.Client{Timeout: timeout},
		logger:  logger,
	}
	initCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if _, err := client.request(initCtx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "squidbot",
			"version": "dev",
		},
	}); err != nil {
		return nil, err
	}
	_ = client.notify(initCtx, "notifications/initialized", map[string]any{})
	return client, nil
}

func (c *httpRPCClient) ListTools(ctx context.Context) ([]remoteTool, error) {
	result, err := c.request(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	return parseToolsList(result)
}

func (c *httpRPCClient) CallTool(ctx context.Context, name string, args map[string]any) (callResult, error) {
	result, err := c.request(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return callResult{}, err
	}
	return parseCallResult(result)
}

func (c *httpRPCClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := atomic.AddInt64(&c.nextID, 1)
	reqBody, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	for key, value := range c.headers {
		httpReq.Header.Set(key, value)
	}
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("mcp http %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	ct := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	var raw []byte
	if strings.Contains(ct, "text/event-stream") {
		events, sseErr := readSSEJSONEvents(resp.Body)
		if sseErr != nil {
			return nil, sseErr
		}
		for _, event := range events {
			if strings.TrimSpace(event) == "" {
				continue
			}
			var rpcResp rpcResponse
			if err := json.Unmarshal([]byte(event), &rpcResp); err == nil && rpcResp.ID == id {
				if rpcResp.Error != nil {
					return nil, fmt.Errorf("mcp rpc %s failed: %s", method, rpcResp.Error.Message)
				}
				return rpcResp.Result, nil
			}
		}
		return nil, fmt.Errorf("mcp rpc %s returned no response event", method)
	}
	raw, err = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var rpcResp rpcResponse
	if err := json.Unmarshal(raw, &rpcResp); err != nil {
		return nil, err
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("mcp rpc %s failed: %s", method, rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}

func (c *httpRPCClient) notify(ctx context.Context, method string, params any) error {
	reqBody, err := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for key, value := range c.headers {
		httpReq.Header.Set(key, value)
	}
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("mcp notify %s failed: %d %s", method, resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	return nil
}

func (c *httpRPCClient) Close() error { return nil }

func parseToolsList(raw json.RawMessage) ([]remoteTool, error) {
	var parsed struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	out := make([]remoteTool, 0, len(parsed.Tools))
	for _, tool := range parsed.Tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		schema := tool.InputSchema
		if len(schema) == 0 {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, remoteTool{Name: name, Description: strings.TrimSpace(tool.Description), InputSchema: schema})
	}
	return out, nil
}

func parseCallResult(raw json.RawMessage) (callResult, error) {
	var parsed struct {
		Content           []map[string]any `json:"content"`
		StructuredContent any              `json:"structuredContent"`
		Metadata          map[string]any   `json:"_meta"`
		IsError           bool             `json:"isError"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return callResult{}, err
	}
	parts := make([]string, 0, len(parsed.Content))
	for _, block := range parsed.Content {
		typeName := strings.ToLower(strings.TrimSpace(asString(block["type"])))
		switch typeName {
		case "text":
			text := strings.TrimSpace(asString(block["text"]))
			if text != "" {
				parts = append(parts, text)
			}
		default:
			encoded, _ := json.Marshal(block)
			if len(encoded) > 0 {
				parts = append(parts, string(encoded))
			}
		}
	}
	if len(parts) == 0 && parsed.StructuredContent != nil {
		encoded, _ := json.Marshal(parsed.StructuredContent)
		if len(encoded) > 0 {
			parts = append(parts, string(encoded))
		}
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	if text == "" {
		text = "(no output)"
	}
	if parsed.IsError {
		return callResult{}, fmt.Errorf("mcp tool error: %s", text)
	}
	return callResult{Text: text, Metadata: parsed.Metadata}, nil
}

func readRPCFrame(reader *bufio.Reader) ([]byte, error) {
	contentLength := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(trimmed), "content-length:") {
			raw := strings.TrimSpace(strings.TrimPrefix(trimmed, "Content-Length:"))
			raw = strings.TrimSpace(strings.TrimPrefix(raw, "content-length:"))
			parsed, convErr := strconv.Atoi(raw)
			if convErr != nil || parsed <= 0 {
				return nil, fmt.Errorf("invalid content-length header")
			}
			contentLength = parsed
		}
	}
	if contentLength <= 0 {
		return nil, fmt.Errorf("missing content-length header")
	}
	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func readSSEJSONEvents(reader io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	buf := make([]string, 0, 4)
	out := []string{}
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			if len(buf) > 0 {
				out = append(out, strings.Join(buf, "\n"))
				buf = buf[:0]
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			buf = append(buf, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(buf) > 0 {
		out = append(out, strings.Join(buf, "\n"))
	}
	return out, nil
}

func cloneHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		k := strings.TrimSpace(key)
		v := strings.TrimSpace(value)
		if k == "" || v == "" {
			continue
		}
		out[k] = v
	}
	return out
}

func asString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func defaultErrMsg(err error) string {
	if err == nil {
		return "transport closed"
	}
	return err.Error()
}
