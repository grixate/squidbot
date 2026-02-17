package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type ChannelRecallFunc func(ctx context.Context, limit int) ([]string, error)

type ChannelRecallTool struct {
	recall ChannelRecallFunc
}

func NewChannelRecallTool(recall ChannelRecallFunc) *ChannelRecallTool {
	return &ChannelRecallTool{recall: recall}
}

func (t *ChannelRecallTool) Name() string { return "channel_recall" }

func (t *ChannelRecallTool) Description() string {
	return "Read recent channel conversation turns (read-only)."
}

func (t *ChannelRecallTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit": map[string]any{"type": "integer"},
		},
	}
}

func (t *ChannelRecallTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.recall == nil {
		return ToolResult{}, fmt.Errorf("channel recall is not configured")
	}
	var in struct {
		Limit int `json:"limit"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
		}
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 12
	}
	lines, err := t.recall(ctx, limit)
	if err != nil {
		return ToolResult{}, err
	}
	if len(lines) == 0 {
		return ToolResult{Text: "No recent channel context found."}, nil
	}
	return ToolResult{
		Text: strings.Join(lines, "\n"),
		Metadata: map[string]any{
			"count": len(lines),
		},
	}, nil
}
