package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type SubagentResultRequest struct {
	RunID     string
	SessionID string
}

type SubagentResultResponse struct {
	Summary       string
	Output        string
	ArtifactPaths []string
	Status        string
	Attempt       int
}

type SubagentResultFunc func(ctx context.Context, req SubagentResultRequest) (SubagentResultResponse, error)

type SubagentResultTool struct {
	result    SubagentResultFunc
	sessionID string
}

func NewSubagentResultTool(result SubagentResultFunc) *SubagentResultTool {
	return &SubagentResultTool{result: result}
}

func (t *SubagentResultTool) SetContext(sessionID string) {
	t.sessionID = sessionID
}

func (t *SubagentResultTool) Name() string { return "subagent_result" }

func (t *SubagentResultTool) Description() string {
	return "Get final result details from a completed subagent run."
}

func (t *SubagentResultTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"run_id": map[string]any{"type": "string"},
	}, "required": []string{"run_id"}}
}

func (t *SubagentResultTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.result == nil {
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
	out, err := t.result(ctx, SubagentResultRequest{RunID: in.RunID, SessionID: t.sessionID})
	if err != nil {
		return ToolResult{}, err
	}
	text := out.Output
	if strings.TrimSpace(text) == "" {
		text = out.Summary
	}
	if strings.TrimSpace(text) == "" {
		text = fmt.Sprintf("Run %s completed with status %s", in.RunID, out.Status)
	}
	return ToolResult{
		Text: text,
		Metadata: map[string]any{
			"status":         out.Status,
			"attempt":        out.Attempt,
			"summary":        out.Summary,
			"artifact_paths": out.ArtifactPaths,
		},
	}, nil
}
