package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/grixate/squidbot/internal/subagent"
)

type SpawnRequest struct {
	Task        string
	Label       string
	Target      string
	ContextMode subagent.ContextMode
	Attachments []string
	TimeoutSec  int
	MaxAttempts int
	Wait        bool
	RequiredCapabilities []string
	PreferredRoles       []string
	PreferredPeerID      string
	AllowFallback        *bool
	SessionID   string
	Channel     string
	ChatID      string
	SenderID    string
	Depth       int
}

type SpawnResponse struct {
	RunID  string
	Status subagent.Status
	Result *subagent.Result
	Text   string
}

type SpawnFunc func(ctx context.Context, req SpawnRequest) (SpawnResponse, error)

type SpawnTool struct {
	spawn     SpawnFunc
	sessionID string
	channel   string
	chatID    string
	senderID  string
	depth     int
}

func NewSpawnTool(spawn SpawnFunc) *SpawnTool {
	return &SpawnTool{spawn: spawn}
}

func (t *SpawnTool) SetContext(sessionID, channel, chatID, senderID string, depth int) {
	t.sessionID = sessionID
	t.channel = channel
	t.chatID = chatID
	t.senderID = senderID
	t.depth = depth
}

func (t *SpawnTool) Name() string { return "spawn" }
func (t *SpawnTool) Description() string {
	return "Spawn a subagent to handle a background task and report back on completion."
}
func (t *SpawnTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"task":         map[string]any{"type": "string"},
		"label":        map[string]any{"type": "string"},
		"target":       map[string]any{"type": "string", "enum": []string{"local", "remote", "auto"}},
		"context_mode": map[string]any{"type": "string", "enum": []string{"minimal", "session", "session_memory"}},
		"attachments":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"timeout_sec":  map[string]any{"type": "integer"},
		"max_attempts": map[string]any{"type": "integer"},
		"wait":         map[string]any{"type": "boolean"},
		"required_capabilities": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"preferred_roles":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"preferred_peer_id":     map[string]any{"type": "string"},
		"allow_fallback":        map[string]any{"type": "boolean"},
	}, "required": []string{"task"}}
}
func (t *SpawnTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.spawn == nil {
		return ToolResult{}, fmt.Errorf("spawn manager is not configured")
	}
	var in struct {
		Task        string   `json:"task"`
		Label       string   `json:"label"`
		Target      string   `json:"target"`
		ContextMode string   `json:"context_mode"`
		Attachments []string `json:"attachments"`
		TimeoutSec  int      `json:"timeout_sec"`
		MaxAttempts int      `json:"max_attempts"`
		Wait        bool     `json:"wait"`
		RequiredCapabilities []string `json:"required_capabilities"`
		PreferredRoles       []string `json:"preferred_roles"`
		PreferredPeerID      string   `json:"preferred_peer_id"`
		AllowFallback        *bool    `json:"allow_fallback"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(in.Task) == "" {
		return ToolResult{}, fmt.Errorf("task is required")
	}
	message, err := t.spawn(ctx, SpawnRequest{
		Task:        in.Task,
		Label:       in.Label,
		Target:      strings.ToLower(strings.TrimSpace(in.Target)),
		ContextMode: subagent.NormalizeContextMode(in.ContextMode),
		Attachments: in.Attachments,
		TimeoutSec:  in.TimeoutSec,
		MaxAttempts: in.MaxAttempts,
		Wait:        in.Wait,
		RequiredCapabilities: in.RequiredCapabilities,
		PreferredRoles:       in.PreferredRoles,
		PreferredPeerID:      in.PreferredPeerID,
		AllowFallback:        in.AllowFallback,
		SessionID:   t.sessionID,
		Channel:     t.channel,
		ChatID:      t.chatID,
		SenderID:    t.senderID,
		Depth:       t.depth,
	})
	if err != nil {
		return ToolResult{}, err
	}
	result := ToolResult{
		Text: message.Text,
		Metadata: map[string]any{
			"run_id": message.RunID,
			"status": message.Status,
		},
	}
	if result.Text == "" {
		result.Text = fmt.Sprintf("Subagent run %s status: %s", message.RunID, message.Status)
	}
	if message.Result != nil {
		result.Metadata["summary"] = message.Result.Summary
		result.Metadata["artifact_paths"] = message.Result.ArtifactPaths
	}
	return result, nil
}
