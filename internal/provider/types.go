package provider

import (
	"context"
	"encoding/json"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"schema"`
}

type ChatRequest struct {
	Messages    []Message
	Tools       []ToolDefinition
	Model       string
	MaxTokens   int
	Temperature float64
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ChatResponse struct {
	Content      string
	ToolCalls    []ToolCall
	FinishReason string
	Usage        Usage
}

func (r ChatResponse) HasToolCalls() bool {
	return len(r.ToolCalls) > 0
}

type StreamEvent struct {
	DeltaContent string
	ToolCall     *ToolCall
	Done         bool
}

type ProviderCapabilities struct {
	SupportsTools   bool
	SupportsStream  bool
	SupportsJSONOut bool
}

type LLMProvider interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
	Stream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, <-chan error)
	Capabilities() ProviderCapabilities
}
