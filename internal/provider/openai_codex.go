package provider

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/oauth"
)

type OpenAICodexProvider struct {
	tokenManager *oauth.TokenManager
	baseURL      string
	accountID    string
	client       *http.Client
}

func NewOpenAICodexProvider(tokenManager *oauth.TokenManager, baseURL, accountID string) *OpenAICodexProvider {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		trimmed = config.ProviderOpenAICodexDefaultAPIBase
	}
	if tokenManager == nil {
		tokenManager = oauth.NewOpenAICodexTokenManager()
	}
	return &OpenAICodexProvider{
		tokenManager: tokenManager,
		baseURL:      trimmed,
		accountID:    strings.TrimSpace(accountID),
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (p *OpenAICodexProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{SupportsTools: true, SupportsStream: true, SupportsJSONOut: true}
}

func (p *OpenAICodexProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	resp, err := p.run(ctx, req, nil)
	if err != nil {
		return ChatResponse{}, err
	}
	return resp, nil
}

func (p *OpenAICodexProvider) Stream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, <-chan error) {
	events := make(chan StreamEvent, 16)
	errs := make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errs)
		_, err := p.run(ctx, req, func(event StreamEvent) {
			events <- event
		})
		if err != nil {
			errs <- err
			return
		}
		events <- StreamEvent{Done: true}
	}()
	return events, errs
}

func (p *OpenAICodexProvider) run(ctx context.Context, req ChatRequest, onEvent func(StreamEvent)) (ChatResponse, error) {
	token, err := p.tokenManager.GetValidToken(ctx)
	if err != nil {
		return ChatResponse{}, err
	}
	instructions, input := codexConvertMessages(req.Messages)
	body := map[string]any{
		"model":               codexStripModelPrefix(defaultString(req.Model, config.ProviderOpenAICodexDefaultModel)),
		"store":               false,
		"stream":              true,
		"instructions":        instructions,
		"input":               input,
		"tool_choice":         "auto",
		"parallel_tool_calls": true,
		"prompt_cache_key":    codexPromptCacheKey(req.Messages),
		"text": map[string]any{
			"verbosity": "medium",
		},
		"include": []string{"reasoning.encrypted_content"},
	}
	if len(req.Tools) > 0 {
		body["tools"] = codexConvertTools(req.Tools)
	}
	data, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.responsesURL(), bytes.NewReader(data))
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+token.AccessToken)
	if accountID := defaultString(token.AccountID, p.accountID); accountID != "" {
		httpReq.Header.Set("chatgpt-account-id", accountID)
	}
	httpReq.Header.Set("OpenAI-Beta", "responses=experimental")
	httpReq.Header.Set("originator", "squidbot")
	httpReq.Header.Set("User-Agent", "squidbot (go)")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var payload map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&payload)
		return ChatResponse{}, fmt.Errorf("codex provider http %d: %v", resp.StatusCode, payload)
	}
	return codexConsumeSSE(resp.Body, onEvent)
}

func (p *OpenAICodexProvider) responsesURL() string {
	if strings.HasSuffix(p.baseURL, "/responses") {
		return p.baseURL
	}
	return p.baseURL + "/responses"
}

type codexToolCallBuffer struct {
	ItemID    string
	Name      string
	Arguments string
}

func codexConsumeSSE(stream io.Reader, onEvent func(StreamEvent)) (ChatResponse, error) {
	if stream == nil {
		return ChatResponse{}, fmt.Errorf("nil codex response body")
	}
	reader := bufio.NewReader(stream)
	out := ChatResponse{FinishReason: "stop"}
	buffers := map[string]*codexToolCallBuffer{}
	for {
		event, done, err := readSSEEvent(reader)
		if err != nil {
			return ChatResponse{}, err
		}
		if done {
			break
		}
		if strings.TrimSpace(event) == "" || strings.TrimSpace(event) == "[DONE]" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(event), &payload); err != nil {
			continue
		}
		eventType, _ := payload["type"].(string)
		switch eventType {
		case "response.output_text.delta":
			delta, _ := payload["delta"].(string)
			if delta == "" {
				continue
			}
			out.Content += delta
			if onEvent != nil {
				onEvent(StreamEvent{DeltaContent: delta})
			}
		case "response.output_item.added":
			item, _ := payload["item"].(map[string]any)
			if item == nil || asString(item["type"]) != "function_call" {
				continue
			}
			callID := asString(item["call_id"])
			if callID == "" {
				continue
			}
			buffers[callID] = &codexToolCallBuffer{
				ItemID:    asString(item["id"]),
				Name:      asString(item["name"]),
				Arguments: asString(item["arguments"]),
			}
		case "response.function_call_arguments.delta":
			callID := asString(payload["call_id"])
			buf := buffers[callID]
			if buf != nil {
				buf.Arguments += asString(payload["delta"])
			}
		case "response.function_call_arguments.done":
			callID := asString(payload["call_id"])
			buf := buffers[callID]
			if buf != nil {
				buf.Arguments = asString(payload["arguments"])
			}
		case "response.output_item.done":
			item, _ := payload["item"].(map[string]any)
			if item == nil || asString(item["type"]) != "function_call" {
				continue
			}
			callID := asString(item["call_id"])
			if callID == "" {
				continue
			}
			buf := buffers[callID]
			if buf == nil {
				buf = &codexToolCallBuffer{ItemID: asString(item["id"]), Name: asString(item["name"]), Arguments: asString(item["arguments"])}
			}
			args := strings.TrimSpace(buf.Arguments)
			if args == "" {
				args = "{}"
			}
			toolCall := ToolCall{
				ID:        codexJoinCallID(callID, defaultString(buf.ItemID, asString(item["id"]))),
				Name:      defaultString(buf.Name, asString(item["name"])),
				Arguments: json.RawMessage(args),
			}
			if !json.Valid(toolCall.Arguments) {
				wrapped, _ := json.Marshal(map[string]string{"raw": args})
				toolCall.Arguments = wrapped
			}
			out.ToolCalls = append(out.ToolCalls, toolCall)
			if onEvent != nil {
				copyCall := toolCall
				onEvent(StreamEvent{ToolCall: &copyCall})
			}
		case "response.completed":
			responseObj, _ := payload["response"].(map[string]any)
			status := asString(responseObj["status"])
			out.FinishReason = codexFinishReason(status)
			if usageObj, ok := responseObj["usage"].(map[string]any); ok {
				prompt := asInt(usageObj["prompt_tokens"])
				if prompt == 0 {
					prompt = asInt(usageObj["input_tokens"])
				}
				completion := asInt(usageObj["completion_tokens"])
				if completion == 0 {
					completion = asInt(usageObj["output_tokens"])
				}
				total := asInt(usageObj["total_tokens"])
				if total == 0 {
					total = prompt + completion
				}
				out.Usage = Usage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: total}
			}
		case "error", "response.failed":
			return ChatResponse{}, fmt.Errorf("codex response failed")
		}
	}
	if out.FinishReason == "" {
		out.FinishReason = "stop"
	}
	return out, nil
}

func readSSEEvent(reader *bufio.Reader) (string, bool, error) {
	lines := make([]string, 0, 4)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if len(lines) == 0 {
				if err == io.EOF {
					return "", true, nil
				}
				return "", false, err
			}
			break
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			break
		}
		if strings.HasPrefix(trimmed, "data:") {
			lines = append(lines, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
		}
	}
	if len(lines) == 0 {
		return "", false, nil
	}
	return strings.Join(lines, "\n"), false, nil
}

func codexPromptCacheKey(messages []Message) string {
	raw, _ := json.Marshal(messages)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func codexConvertTools(in []ToolDefinition) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, td := range in {
		out = append(out, map[string]any{
			"type":        "function",
			"name":        td.Name,
			"description": td.Description,
			"parameters":  td.Schema,
		})
	}
	return out
}

func codexConvertMessages(messages []Message) (string, []map[string]any) {
	system := ""
	out := make([]map[string]any, 0, len(messages))
	for i, msg := range messages {
		switch msg.Role {
		case "system":
			if system == "" {
				system = msg.Content
			} else if strings.TrimSpace(msg.Content) != "" {
				system += "\n\n" + msg.Content
			}
		case "user":
			out = append(out, map[string]any{
				"role": "user",
				"content": []map[string]any{{
					"type": "input_text",
					"text": msg.Content,
				}},
			})
		case "assistant":
			if strings.TrimSpace(msg.Content) != "" {
				out = append(out, map[string]any{
					"type":   "message",
					"role":   "assistant",
					"status": "completed",
					"id":     fmt.Sprintf("msg_%d", i),
					"content": []map[string]any{{
						"type": "output_text",
						"text": msg.Content,
					}},
				})
			}
			for _, tc := range msg.ToolCalls {
				callID, itemID := codexSplitCallID(tc.ID)
				args := strings.TrimSpace(string(tc.Arguments))
				if args == "" {
					args = "{}"
				}
				out = append(out, map[string]any{
					"type":      "function_call",
					"id":        defaultString(itemID, fmt.Sprintf("fc_%d", i)),
					"call_id":   defaultString(callID, fmt.Sprintf("call_%d", i)),
					"name":      tc.Name,
					"arguments": args,
				})
			}
		case "tool":
			callID, _ := codexSplitCallID(msg.ToolCallID)
			out = append(out, map[string]any{
				"type":    "function_call_output",
				"call_id": defaultString(callID, "call_0"),
				"output":  msg.Content,
			})
		}
	}
	return system, out
}

func codexSplitCallID(id string) (string, string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", ""
	}
	if strings.Contains(id, "|") {
		parts := strings.SplitN(id, "|", 2)
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return id, ""
}

func codexJoinCallID(callID, itemID string) string {
	callID = strings.TrimSpace(callID)
	itemID = strings.TrimSpace(itemID)
	if callID == "" {
		callID = "call_0"
	}
	if itemID == "" {
		return callID
	}
	return callID + "|" + itemID
}

func codexStripModelPrefix(model string) string {
	model = strings.TrimSpace(model)
	if strings.HasPrefix(model, "openai-codex/") {
		return strings.TrimPrefix(model, "openai-codex/")
	}
	return model
}

func codexFinishReason(status string) string {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "completed", "":
		return "stop"
	case "incomplete":
		return "length"
	case "failed", "cancelled":
		return "error"
	default:
		return "stop"
	}
}

func asString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return ""
	}
}

func asInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		parsed, err := v.Int64()
		if err == nil {
			return int(parsed)
		}
	case string:
		parsed, err := json.Number(strings.TrimSpace(v)).Int64()
		if err == nil {
			return int(parsed)
		}
	}
	return 0
}

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}
