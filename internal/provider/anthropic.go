package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type AnthropicProvider struct {
	apiKey string
	model  string
	client *http.Client
}

func NewAnthropicProvider(apiKey, model string) *AnthropicProvider {
	if strings.TrimSpace(model) == "" {
		model = "claude-3-5-sonnet-20241022"
	}
	return &AnthropicProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *AnthropicProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{SupportsTools: true, SupportsStream: false, SupportsJSONOut: false}
}

func (p *AnthropicProvider) Stream(_ context.Context, _ ChatRequest) (<-chan StreamEvent, <-chan error) {
	events := make(chan StreamEvent)
	errs := make(chan error, 1)
	close(events)
	errs <- fmt.Errorf("streaming not implemented")
	close(errs)
	return events, errs
}

func (p *AnthropicProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	system := ""
	messages := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		if m.Role == "system" {
			if system == "" {
				system = m.Content
			}
			continue
		}
		messages = append(messages, map[string]any{
			"role":    m.Role,
			"content": m.Content,
		})
	}

	payload := map[string]any{
		"model":       req.Model,
		"max_tokens":  req.MaxTokens,
		"temperature": req.Temperature,
		"system":      system,
		"messages":    messages,
	}

	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"name":         t.Name,
				"description":  t.Description,
				"input_schema": t.Schema,
			})
		}
		payload["tools"] = tools
	}

	if strings.TrimSpace(payload["model"].(string)) == "" {
		payload["model"] = p.model
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return ChatResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(data))
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("content-type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return ChatResponse{}, fmt.Errorf("provider http %d: %v", resp.StatusCode, body)
	}

	var parsed anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return ChatResponse{}, err
	}

	out := ChatResponse{FinishReason: parsed.StopReason}
	for _, block := range parsed.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				if out.Content != "" {
					out.Content += "\n"
				}
				out.Content += block.Text
			}
		case "tool_use":
			args, _ := json.Marshal(block.Input)
			if len(args) == 0 {
				args = []byte("{}")
			}
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: json.RawMessage(args),
			})
		}
	}
	out.Usage = Usage{
		PromptTokens:     parsed.Usage.InputTokens,
		CompletionTokens: parsed.Usage.OutputTokens,
		TotalTokens:      parsed.Usage.InputTokens + parsed.Usage.OutputTokens,
	}
	return out, nil
}

type anthropicResponse struct {
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Content []struct {
		Type  string         `json:"type"`
		Text  string         `json:"text"`
		ID    string         `json:"id"`
		Name  string         `json:"name"`
		Input map[string]any `json:"input"`
	} `json:"content"`
}
