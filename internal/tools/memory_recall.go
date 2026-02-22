package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type MemoryRecallFunc func(ctx context.Context, query string, limit int) ([]string, error)

type MemoryRecallTool struct {
	recall MemoryRecallFunc
}

func NewMemoryRecallTool(recall MemoryRecallFunc) *MemoryRecallTool {
	return &MemoryRecallTool{recall: recall}
}

func (t *MemoryRecallTool) Name() string { return "memory_recall" }

func (t *MemoryRecallTool) Description() string {
	return "Recall matching memory snippets for reasoning."
}

func (t *MemoryRecallTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
			"limit": map[string]any{"type": "integer"},
		},
		"required": []string{"query"},
	}
}

func (t *MemoryRecallTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.recall == nil {
		return ToolResult{}, fmt.Errorf("memory recall is not configured")
	}
	var in struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return ToolResult{}, fmt.Errorf("query is required")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 6
	}
	matches, err := t.recall(ctx, query, limit)
	if err != nil {
		return ToolResult{}, err
	}
	if len(matches) == 0 {
		return ToolResult{Text: "No memory matches found."}, nil
	}
	return ToolResult{
		Text: "- " + strings.Join(matches, "\n- "),
		Metadata: map[string]any{
			"count": len(matches),
		},
	}, nil
}
