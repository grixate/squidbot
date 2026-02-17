package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/grixate/squidbot/internal/branch"
)

type BranchSpawnRequest struct {
	Description string
	Prompt      string
	Wait        bool
	TimeoutSec  int
	SessionID   string
}

type BranchSpawnFunc func(ctx context.Context, req BranchSpawnRequest) (branch.BranchRun, error)

type BranchSpawnTool struct {
	spawn     BranchSpawnFunc
	sessionID string
}

func NewBranchSpawnTool(spawn BranchSpawnFunc) *BranchSpawnTool {
	return &BranchSpawnTool{spawn: spawn}
}

func (t *BranchSpawnTool) SetContext(sessionID string) {
	t.sessionID = strings.TrimSpace(sessionID)
}

func (t *BranchSpawnTool) Name() string { return "branch_spawn" }

func (t *BranchSpawnTool) Description() string {
	return "Spawn a lightweight branch for reasoning-only work."
}

func (t *BranchSpawnTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"description": map[string]any{"type": "string"},
			"prompt":      map[string]any{"type": "string"},
			"wait":        map[string]any{"type": "boolean"},
			"timeout_sec": map[string]any{"type": "integer"},
		},
		"required": []string{"prompt"},
	}
}

func (t *BranchSpawnTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.spawn == nil {
		return ToolResult{}, fmt.Errorf("branch manager is not configured")
	}
	var in struct {
		Description string `json:"description"`
		Prompt      string `json:"prompt"`
		Wait        bool   `json:"wait"`
		TimeoutSec  int    `json:"timeout_sec"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(in.Prompt) == "" {
		return ToolResult{}, fmt.Errorf("prompt is required")
	}
	run, err := t.spawn(ctx, BranchSpawnRequest{
		Description: in.Description,
		Prompt:      in.Prompt,
		Wait:        in.Wait,
		TimeoutSec:  in.TimeoutSec,
		SessionID:   t.sessionID,
	})
	if err != nil {
		return ToolResult{}, err
	}
	text := fmt.Sprintf("Branch %s is %s.", run.ID, run.Status)
	if strings.TrimSpace(run.Conclusion) != "" {
		text += "\n" + strings.TrimSpace(run.Conclusion)
	}
	return ToolResult{
		Text: text,
		Metadata: map[string]any{
			"branch_id":   run.ID,
			"status":      run.Status,
			"description": run.Description,
			"model":       run.Model,
		},
	}, nil
}
