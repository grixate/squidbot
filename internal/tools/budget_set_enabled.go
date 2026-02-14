package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/grixate/squidbot/internal/budget"
)

type BudgetSetEnabledRequest struct {
	SessionID string
	Channel   string
	SenderID  string
	Enabled   bool
}

type BudgetSetEnabledResponse struct {
	Settings budget.Settings
}

type BudgetSetEnabledFunc func(ctx context.Context, req BudgetSetEnabledRequest) (BudgetSetEnabledResponse, error)

type BudgetSetEnabledTool struct {
	set       BudgetSetEnabledFunc
	sessionID string
	channel   string
	senderID  string
}

func NewBudgetSetEnabledTool(set BudgetSetEnabledFunc) *BudgetSetEnabledTool {
	return &BudgetSetEnabledTool{set: set}
}

func (t *BudgetSetEnabledTool) SetContext(sessionID, channel, senderID string) {
	t.sessionID = sessionID
	t.channel = channel
	t.senderID = senderID
}

func (t *BudgetSetEnabledTool) Name() string { return "budget_set_enabled" }

func (t *BudgetSetEnabledTool) Description() string {
	return "Enable or disable token safety enforcement."
}

func (t *BudgetSetEnabledTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"enabled": map[string]any{"type": "boolean"},
		},
		"required": []string{"enabled"},
	}
}

func (t *BudgetSetEnabledTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.set == nil {
		return ToolResult{}, fmt.Errorf("budget manager is not configured")
	}
	var in struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if in.Enabled == nil {
		return ToolResult{}, fmt.Errorf("enabled is required")
	}
	out, err := t.set(ctx, BudgetSetEnabledRequest{
		SessionID: t.sessionID,
		Channel:   t.channel,
		SenderID:  t.senderID,
		Enabled:   *in.Enabled,
	})
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{
		Text: fmt.Sprintf("Token safety enabled=%v.", out.Settings.Enabled),
		Metadata: map[string]any{
			"settings": out.Settings,
		},
	}, nil
}
