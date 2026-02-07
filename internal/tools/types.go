package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/grixate/squidbot/internal/provider"
)

type ToolResult struct {
	Text      string         `json:"text"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Retryable bool           `json:"retryable,omitempty"`
}

type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any
	Execute(ctx context.Context, args json.RawMessage) (ToolResult, error)
}

type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

func (r *Registry) Register(tool Tool) {
	r.tools[tool.Name()] = tool
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) Execute(ctx context.Context, name string, args json.RawMessage) (ToolResult, error) {
	tool, ok := r.tools[name]
	if !ok {
		return ToolResult{}, fmt.Errorf("Error: Tool '%s' not found", name)
	}
	result, err := tool.Execute(ctx, args)
	if err != nil {
		return ToolResult{}, fmt.Errorf("Error executing %s: %w", name, err)
	}
	return result, nil
}

func (r *Registry) Definitions() []provider.ToolDefinition {
	out := make([]provider.ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, provider.ToolDefinition{Name: t.Name(), Description: t.Description(), Schema: t.Schema()})
	}
	return out
}

func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.tools))
	for name := range r.tools {
		out = append(out, name)
	}
	return out
}
