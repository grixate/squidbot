package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type SubagentCancelRequest struct {
	RunID     string
	SessionID string
}

type SubagentCancelResponse struct {
	RunID  string
	Status string
}

type SubagentCancelFunc func(ctx context.Context, req SubagentCancelRequest) (SubagentCancelResponse, error)

type SubagentCancelTool struct {
	cancel    SubagentCancelFunc
	sessionID string
}

func NewSubagentCancelTool(cancel SubagentCancelFunc) *SubagentCancelTool {
	return &SubagentCancelTool{cancel: cancel}
}

func (t *SubagentCancelTool) SetContext(sessionID string) {
	t.sessionID = sessionID
}

func (t *SubagentCancelTool) Name() string { return "subagent_cancel" }

func (t *SubagentCancelTool) Description() string {
	return "Cancel a queued or running subagent run."
}

func (t *SubagentCancelTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"run_id": map[string]any{"type": "string"},
	}, "required": []string{"run_id"}}
}

func (t *SubagentCancelTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.cancel == nil {
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
	out, err := t.cancel(ctx, SubagentCancelRequest{RunID: in.RunID, SessionID: t.sessionID})
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Text: fmt.Sprintf("Run %s set to %s", out.RunID, out.Status), Metadata: map[string]any{"run_id": out.RunID, "status": out.Status}}, nil
}
