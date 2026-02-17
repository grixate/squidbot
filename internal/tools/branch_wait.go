package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/grixate/squidbot/internal/branch"
)

type BranchWaitRequest struct {
	BranchID  string
	TimeoutSec int
	SessionID string
}

type BranchWaitFunc func(ctx context.Context, req BranchWaitRequest) (branch.BranchRun, error)

type BranchWaitTool struct {
	wait      BranchWaitFunc
	sessionID string
}

func NewBranchWaitTool(wait BranchWaitFunc) *BranchWaitTool {
	return &BranchWaitTool{wait: wait}
}

func (t *BranchWaitTool) SetContext(sessionID string) {
	t.sessionID = strings.TrimSpace(sessionID)
}

func (t *BranchWaitTool) Name() string { return "branch_wait" }

func (t *BranchWaitTool) Description() string {
	return "Wait for a branch to complete and return its conclusion."
}

func (t *BranchWaitTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"branch_id":  map[string]any{"type": "string"},
			"timeout_sec": map[string]any{"type": "integer"},
		},
		"required": []string{"branch_id"},
	}
}

func (t *BranchWaitTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.wait == nil {
		return ToolResult{}, fmt.Errorf("branch manager is not configured")
	}
	var in struct {
		BranchID  string `json:"branch_id"`
		TimeoutSec int   `json:"timeout_sec"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	run, err := t.wait(ctx, BranchWaitRequest{
		BranchID:  strings.TrimSpace(in.BranchID),
		TimeoutSec: in.TimeoutSec,
		SessionID: t.sessionID,
	})
	if err != nil {
		return ToolResult{}, err
	}
	text := fmt.Sprintf("Branch %s finished with status %s.", run.ID, run.Status)
	if strings.TrimSpace(run.Conclusion) != "" {
		text += "\n" + strings.TrimSpace(run.Conclusion)
	}
	if strings.TrimSpace(run.Error) != "" {
		text += "\nError: " + strings.TrimSpace(run.Error)
	}
	return ToolResult{
		Text: text,
		Metadata: map[string]any{
			"branch_id": run.ID,
			"status":    run.Status,
			"model":     run.Model,
		},
	}, nil
}
