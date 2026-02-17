package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type MemorySaveFunc func(ctx context.Context, entry string) (string, error)

type MemorySaveTool struct {
	save MemorySaveFunc
}

func NewMemorySaveTool(save MemorySaveFunc) *MemorySaveTool {
	return &MemorySaveTool{save: save}
}

func (t *MemorySaveTool) Name() string { return "memory_save" }

func (t *MemorySaveTool) Description() string {
	return "Persist an important memory note."
}

func (t *MemorySaveTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"entry": map[string]any{"type": "string"},
		},
		"required": []string{"entry"},
	}
}

func (t *MemorySaveTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.save == nil {
		return ToolResult{}, fmt.Errorf("memory save is not configured")
	}
	var in struct {
		Entry string `json:"entry"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	entry := strings.TrimSpace(in.Entry)
	if entry == "" {
		return ToolResult{}, fmt.Errorf("entry is required")
	}
	path, err := t.save(ctx, entry)
	if err != nil {
		return ToolResult{}, err
	}
	text := "Memory entry saved."
	if strings.TrimSpace(path) != "" {
		text += " (" + strings.TrimSpace(path) + ")"
	}
	return ToolResult{
		Text: text,
		Metadata: map[string]any{
			"path": path,
		},
	}, nil
}
