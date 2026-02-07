package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type SpawnRequest struct {
	Task      string
	Label     string
	SessionID string
	Channel   string
	ChatID    string
	SenderID  string
}

type SpawnFunc func(ctx context.Context, req SpawnRequest) (string, error)

type SpawnTool struct {
	spawn     SpawnFunc
	sessionID string
	channel   string
	chatID    string
	senderID  string
}

func NewSpawnTool(spawn SpawnFunc) *SpawnTool {
	return &SpawnTool{spawn: spawn}
}

func (t *SpawnTool) SetContext(sessionID, channel, chatID, senderID string) {
	t.sessionID = sessionID
	t.channel = channel
	t.chatID = chatID
	t.senderID = senderID
}

func (t *SpawnTool) Name() string { return "spawn" }
func (t *SpawnTool) Description() string {
	return "Spawn a subagent to handle a background task and report back on completion."
}
func (t *SpawnTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"task":  map[string]any{"type": "string"},
		"label": map[string]any{"type": "string"},
	}, "required": []string{"task"}}
}
func (t *SpawnTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.spawn == nil {
		return ToolResult{}, fmt.Errorf("spawn manager is not configured")
	}
	var in struct {
		Task  string `json:"task"`
		Label string `json:"label"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(in.Task) == "" {
		return ToolResult{}, fmt.Errorf("task is required")
	}
	message, err := t.spawn(ctx, SpawnRequest{
		Task:      in.Task,
		Label:     in.Label,
		SessionID: t.sessionID,
		Channel:   t.channel,
		ChatID:    t.chatID,
		SenderID:  t.senderID,
	})
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Text: message}, nil
}
