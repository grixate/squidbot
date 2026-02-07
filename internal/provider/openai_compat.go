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

type OpenAICompatProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func NewOpenAICompatProvider(apiKey, baseURL string) *OpenAICompatProvider {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		trimmed = "https://api.openai.com/v1"
	}
	return &OpenAICompatProvider{
		apiKey:  apiKey,
		baseURL: trimmed,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (p *OpenAICompatProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{SupportsTools: true, SupportsStream: false, SupportsJSONOut: true}
}

func (p *OpenAICompatProvider) Stream(_ context.Context, _ ChatRequest) (<-chan StreamEvent, <-chan error) {
	events := make(chan StreamEvent)
	errs := make(chan error, 1)
	close(events)
	errs <- fmt.Errorf("streaming not implemented")
	close(errs)
	return events, errs
}

func (p *OpenAICompatProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	payload := map[string]any{
		"model":       req.Model,
		"messages":    toOpenAIMessages(req.Messages),
		"max_tokens":  req.MaxTokens,
		"temperature": req.Temperature,
	}
	if len(req.Tools) > 0 {
		payload["tools"] = toOpenAITools(req.Tools)
		payload["tool_choice"] = "auto"
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return ChatResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

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

	var parsed openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return ChatResponse{}, err
	}
	if len(parsed.Choices) == 0 {
		return ChatResponse{}, fmt.Errorf("provider returned no choices")
	}
	choice := parsed.Choices[0]
	out := ChatResponse{
		Content:      choice.Message.Content,
		FinishReason: choice.FinishReason,
		Usage: Usage{
			PromptTokens:     parsed.Usage.PromptTokens,
			CompletionTokens: parsed.Usage.CompletionTokens,
			TotalTokens:      parsed.Usage.TotalTokens,
		},
	}

	for _, tc := range choice.Message.ToolCalls {
		args := json.RawMessage(tc.Function.Arguments)
		if len(args) == 0 {
			args = json.RawMessage("{}")
		}
		out.ToolCalls = append(out.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: args})
	}
	return out, nil
}

func toOpenAIMessages(in []Message) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, msg := range in {
		rec := map[string]any{"role": msg.Role, "content": msg.Content}
		if msg.Name != "" {
			rec["name"] = msg.Name
		}
		if msg.ToolCallID != "" {
			rec["tool_call_id"] = msg.ToolCallID
		}
		if len(msg.ToolCalls) > 0 {
			tcs := make([]map[string]any, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				tcs = append(tcs, map[string]any{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]any{
						"name":      tc.Name,
						"arguments": string(tc.Arguments),
					},
				})
			}
			rec["tool_calls"] = tcs
		}
		out = append(out, rec)
	}
	return out
}

func toOpenAITools(in []ToolDefinition) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, td := range in {
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        td.Name,
				"description": td.Description,
				"parameters":  td.Schema,
			},
		})
	}
	return out
}

type openAIResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}
