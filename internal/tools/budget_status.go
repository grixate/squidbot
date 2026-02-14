package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/grixate/squidbot/internal/budget"
)

type BudgetStatusRequest struct {
	SessionID string
	Channel   string
	SenderID  string
	RunID     string
}

type BudgetStatusScope struct {
	Scope        string
	Used         uint64
	Reserved     uint64
	HardLimit    uint64
	WarningLevel int
	SoftWarning  bool
	HardExceeded bool
}

type BudgetStatusResponse struct {
	Settings budget.Settings
	Scopes   []BudgetStatusScope
}

type BudgetStatusFunc func(ctx context.Context, req BudgetStatusRequest) (BudgetStatusResponse, error)

type BudgetStatusTool struct {
	status    BudgetStatusFunc
	sessionID string
	channel   string
	senderID  string
}

func NewBudgetStatusTool(status BudgetStatusFunc) *BudgetStatusTool {
	return &BudgetStatusTool{status: status}
}

func (t *BudgetStatusTool) SetContext(sessionID, channel, senderID string) {
	t.sessionID = sessionID
	t.channel = channel
	t.senderID = senderID
}

func (t *BudgetStatusTool) Name() string { return "budget_status" }

func (t *BudgetStatusTool) Description() string {
	return "Show token safety mode, limits, and current usage status."
}

func (t *BudgetStatusTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"run_id": map[string]any{"type": "string"},
		},
	}
}

func (t *BudgetStatusTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.status == nil {
		return ToolResult{}, fmt.Errorf("budget status manager is not configured")
	}
	var in struct {
		RunID string `json:"run_id"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
		}
	}
	out, err := t.status(ctx, BudgetStatusRequest{
		SessionID: t.sessionID,
		Channel:   t.channel,
		SenderID:  t.senderID,
		RunID:     strings.TrimSpace(in.RunID),
	})
	if err != nil {
		return ToolResult{}, err
	}
	lines := []string{
		fmt.Sprintf("Token safety: enabled=%v mode=%s", out.Settings.Enabled, out.Settings.Mode),
		fmt.Sprintf("Limits: global=%d session=%d subagent=%d", out.Settings.GlobalHardLimitTokens, out.Settings.SessionHardLimitTokens, out.Settings.SubagentRunHardLimitTokens),
	}
	for _, scope := range out.Scopes {
		lines = append(lines, fmt.Sprintf("%s: used=%d reserved=%d hard_limit=%d soft_warning=%v hard_exceeded=%v",
			scope.Scope, scope.Used, scope.Reserved, scope.HardLimit, scope.SoftWarning, scope.HardExceeded))
	}
	return ToolResult{
		Text: strings.Join(lines, "\n"),
		Metadata: map[string]any{
			"settings": out.Settings,
			"scopes":   out.Scopes,
		},
	}, nil
}
