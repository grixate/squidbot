package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type MemoryDeleteFunc func(ctx context.Context, query string) (int, error)

type MemoryDeleteTool struct {
	remove MemoryDeleteFunc
}

func NewMemoryDeleteTool(remove MemoryDeleteFunc) *MemoryDeleteTool {
	return &MemoryDeleteTool{remove: remove}
}

func (t *MemoryDeleteTool) Name() string { return "memory_delete" }

func (t *MemoryDeleteTool) Description() string {
	return "Delete memory entries matching a query."
}

func (t *MemoryDeleteTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
		},
		"required": []string{"query"},
	}
}

func (t *MemoryDeleteTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.remove == nil {
		return ToolResult{}, fmt.Errorf("memory delete is not configured")
	}
	var in struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return ToolResult{}, fmt.Errorf("query is required")
	}
	removed, err := t.remove(ctx, query)
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{
		Text: fmt.Sprintf("Removed %d memory entries.", removed),
		Metadata: map[string]any{
			"removed": removed,
		},
	}, nil
}
