package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/grixate/squidbot/internal/branch"
)

type BranchStatusRequest struct {
	BranchID  string
	SessionID string
}

type BranchStatusFunc func(ctx context.Context, req BranchStatusRequest) (branch.BranchRun, error)

type BranchStatusTool struct {
	status    BranchStatusFunc
	sessionID string
}

func NewBranchStatusTool(status BranchStatusFunc) *BranchStatusTool {
	return &BranchStatusTool{status: status}
}

func (t *BranchStatusTool) SetContext(sessionID string) {
	t.sessionID = strings.TrimSpace(sessionID)
}

func (t *BranchStatusTool) Name() string { return "branch_status" }

func (t *BranchStatusTool) Description() string {
	return "Get status of a branch by branch_id."
}

func (t *BranchStatusTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"branch_id": map[string]any{"type": "string"},
		},
		"required": []string{"branch_id"},
	}
}

func (t *BranchStatusTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.status == nil {
		return ToolResult{}, fmt.Errorf("branch manager is not configured")
	}
	var in struct {
		BranchID string `json:"branch_id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	run, err := t.status(ctx, BranchStatusRequest{
		BranchID:  strings.TrimSpace(in.BranchID),
		SessionID: t.sessionID,
	})
	if err != nil {
		return ToolResult{}, err
	}
	text := fmt.Sprintf("Branch %s status: %s.", run.ID, run.Status)
	if strings.TrimSpace(run.Conclusion) != "" {
		text += "\n" + strings.TrimSpace(run.Conclusion)
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
