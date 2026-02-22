package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/grixate/squidbot/internal/subagent"
)

type SubagentListRequest struct {
	Status    string
	Limit     int
	SessionID string
}

type SubagentListResponse struct {
	Runs []subagent.Run
}

type SubagentListFunc func(ctx context.Context, req SubagentListRequest) (SubagentListResponse, error)

type SubagentListTool struct {
	list      SubagentListFunc
	sessionID string
}

func NewSubagentListTool(list SubagentListFunc) *SubagentListTool {
	return &SubagentListTool{list: list}
}

func (t *SubagentListTool) SetContext(sessionID string) {
	t.sessionID = sessionID
}

func (t *SubagentListTool) Name() string { return "subagent_list" }

func (t *SubagentListTool) Description() string {
	return "List subagent runs for the current session. Optionally filter by status."
}

func (t *SubagentListTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"status": map[string]any{"type": "string", "enum": []string{
			"all",
			"active",
			string(subagent.StatusQueued),
			string(subagent.StatusRunning),
			string(subagent.StatusSucceeded),
			string(subagent.StatusFailed),
			string(subagent.StatusTimedOut),
			string(subagent.StatusCancelled),
		}},
		"limit": map[string]any{"type": "integer"},
	}}
}

func (t *SubagentListTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.list == nil {
		return ToolResult{}, fmt.Errorf("subagent manager is not configured")
	}
	var in struct {
		Status string `json:"status"`
		Limit  int    `json:"limit"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
		}
	}
	out, err := t.list(ctx, SubagentListRequest{
		Status:    in.Status,
		Limit:     in.Limit,
		SessionID: t.sessionID,
	})
	if err != nil {
		return ToolResult{}, err
	}

	if len(out.Runs) == 0 {
		return ToolResult{
			Text:     "No subagent runs found.",
			Metadata: map[string]any{"runs": []map[string]any{}},
		}, nil
	}

	summaries := make([]string, 0, len(out.Runs))
	metaRuns := make([]map[string]any, 0, len(out.Runs))
	for _, run := range out.Runs {
		summaries = append(summaries, fmt.Sprintf("%s=%s", run.ID, run.Status))
		metaRuns = append(metaRuns, map[string]any{
			"id":           run.ID,
			"status":       run.Status,
			"label":        run.Label,
			"task":         run.Task,
			"attempt":      run.Attempt,
			"max_attempts": run.MaxAttempts,
			"created_at":   run.CreatedAt,
			"error":        run.Error,
		})
	}

	return ToolResult{
		Text:     "Runs: " + strings.Join(summaries, ", "),
		Metadata: map[string]any{"runs": metaRuns},
	}, nil
}
