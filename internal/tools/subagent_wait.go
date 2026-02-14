package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/grixate/squidbot/internal/subagent"
)

type SubagentWaitRequest struct {
	RunIDs     []string
	TimeoutSec int
	SessionID  string
}

type SubagentWaitResponse struct {
	Runs []subagent.Run
}

type SubagentWaitFunc func(ctx context.Context, req SubagentWaitRequest) (SubagentWaitResponse, error)

type SubagentWaitTool struct {
	wait      SubagentWaitFunc
	sessionID string
}

func NewSubagentWaitTool(wait SubagentWaitFunc) *SubagentWaitTool {
	return &SubagentWaitTool{wait: wait}
}

func (t *SubagentWaitTool) SetContext(sessionID string) {
	t.sessionID = sessionID
}

func (t *SubagentWaitTool) Name() string { return "subagent_wait" }

func (t *SubagentWaitTool) Description() string {
	return "Wait for one or more subagent runs to complete and return their final statuses."
}

func (t *SubagentWaitTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"run_ids":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"timeout_sec": map[string]any{"type": "integer"},
	}, "required": []string{"run_ids"}}
}

func (t *SubagentWaitTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.wait == nil {
		return ToolResult{}, fmt.Errorf("subagent manager is not configured")
	}
	var in struct {
		RunIDs     []string `json:"run_ids"`
		TimeoutSec int      `json:"timeout_sec"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if len(in.RunIDs) == 0 {
		return ToolResult{}, fmt.Errorf("run_ids is required")
	}
	out, err := t.wait(ctx, SubagentWaitRequest{RunIDs: in.RunIDs, TimeoutSec: in.TimeoutSec, SessionID: t.sessionID})
	if err != nil {
		return ToolResult{}, err
	}
	summaries := make([]string, 0, len(out.Runs))
	metaRuns := make([]map[string]any, 0, len(out.Runs))
	for _, run := range out.Runs {
		summaries = append(summaries, fmt.Sprintf("%s=%s", run.ID, run.Status))
		metaRuns = append(metaRuns, map[string]any{"id": run.ID, "status": run.Status, "attempt": run.Attempt, "error": run.Error})
	}
	return ToolResult{
		Text:     "Runs: " + strings.Join(summaries, ", "),
		Metadata: map[string]any{"runs": metaRuns},
	}, nil
}
