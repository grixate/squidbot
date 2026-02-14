package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/grixate/squidbot/internal/subagent"
)

type SubagentStatusRequest struct {
	RunID     string
	SessionID string
}

type SubagentStatusResponse struct {
	Run subagent.Run
}

type SubagentStatusFunc func(ctx context.Context, req SubagentStatusRequest) (SubagentStatusResponse, error)

type SubagentStatusTool struct {
	status    SubagentStatusFunc
	sessionID string
}

func NewSubagentStatusTool(status SubagentStatusFunc) *SubagentStatusTool {
	return &SubagentStatusTool{status: status}
}

func (t *SubagentStatusTool) SetContext(sessionID string) {
	t.sessionID = sessionID
}

func (t *SubagentStatusTool) Name() string { return "subagent_status" }

func (t *SubagentStatusTool) Description() string {
	return "Get status and details for a subagent run."
}

func (t *SubagentStatusTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"run_id": map[string]any{"type": "string"},
	}, "required": []string{"run_id"}}
}

func (t *SubagentStatusTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.status == nil {
		return ToolResult{}, fmt.Errorf("subagent manager is not configured")
	}
	var in struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(in.RunID) == "" {
		return ToolResult{}, fmt.Errorf("run_id is required")
	}
	out, err := t.status(ctx, SubagentStatusRequest{RunID: in.RunID, SessionID: t.sessionID})
	if err != nil {
		return ToolResult{}, err
	}
	meta := map[string]any{
		"run": map[string]any{
			"id":           out.Run.ID,
			"status":       out.Run.Status,
			"attempt":      out.Run.Attempt,
			"max_attempts": out.Run.MaxAttempts,
			"error":        out.Run.Error,
			"task":         out.Run.Task,
		},
	}
	return ToolResult{Text: fmt.Sprintf("Run %s is %s", out.Run.ID, out.Run.Status), Metadata: meta}, nil
}
