package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/grixate/squidbot/internal/budget"
)

type BudgetSetLimitsRequest struct {
	SessionID                   string
	Channel                     string
	SenderID                    string
	GlobalHardLimitTokens       *uint64
	GlobalSoftThresholdPct      *int
	SessionHardLimitTokens      *uint64
	SessionSoftThresholdPct     *int
	SubagentRunHardLimitTokens  *uint64
	SubagentRunSoftThresholdPct *int
}

type BudgetSetLimitsResponse struct {
	Settings budget.Settings
}

type BudgetSetLimitsFunc func(ctx context.Context, req BudgetSetLimitsRequest) (BudgetSetLimitsResponse, error)

type BudgetSetLimitsTool struct {
	set       BudgetSetLimitsFunc
	sessionID string
	channel   string
	senderID  string
}

func NewBudgetSetLimitsTool(set BudgetSetLimitsFunc) *BudgetSetLimitsTool {
	return &BudgetSetLimitsTool{set: set}
}

func (t *BudgetSetLimitsTool) SetContext(sessionID, channel, senderID string) {
	t.sessionID = sessionID
	t.channel = channel
	t.senderID = senderID
}

func (t *BudgetSetLimitsTool) Name() string { return "budget_set_limits" }

func (t *BudgetSetLimitsTool) Description() string {
	return "Update token safety hard limits and soft thresholds."
}

func (t *BudgetSetLimitsTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"global_hard_limit_tokens":        map[string]any{"type": "integer"},
			"global_soft_threshold_pct":       map[string]any{"type": "integer"},
			"session_hard_limit_tokens":       map[string]any{"type": "integer"},
			"session_soft_threshold_pct":      map[string]any{"type": "integer"},
			"subagent_run_hard_limit_tokens":  map[string]any{"type": "integer"},
			"subagent_run_soft_threshold_pct": map[string]any{"type": "integer"},
		},
	}
}

func (t *BudgetSetLimitsTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.set == nil {
		return ToolResult{}, fmt.Errorf("budget manager is not configured")
	}
	var in struct {
		GlobalHardLimitTokens       *uint64 `json:"global_hard_limit_tokens"`
		GlobalSoftThresholdPct      *int    `json:"global_soft_threshold_pct"`
		SessionHardLimitTokens      *uint64 `json:"session_hard_limit_tokens"`
		SessionSoftThresholdPct     *int    `json:"session_soft_threshold_pct"`
		SubagentRunHardLimitTokens  *uint64 `json:"subagent_run_hard_limit_tokens"`
		SubagentRunSoftThresholdPct *int    `json:"subagent_run_soft_threshold_pct"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	out, err := t.set(ctx, BudgetSetLimitsRequest{
		SessionID:                   t.sessionID,
		Channel:                     t.channel,
		SenderID:                    t.senderID,
		GlobalHardLimitTokens:       in.GlobalHardLimitTokens,
		GlobalSoftThresholdPct:      in.GlobalSoftThresholdPct,
		SessionHardLimitTokens:      in.SessionHardLimitTokens,
		SessionSoftThresholdPct:     in.SessionSoftThresholdPct,
		SubagentRunHardLimitTokens:  in.SubagentRunHardLimitTokens,
		SubagentRunSoftThresholdPct: in.SubagentRunSoftThresholdPct,
	})
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{
		Text: "Token safety limits updated.",
		Metadata: map[string]any{
			"settings": out.Settings,
		},
	}, nil
}
