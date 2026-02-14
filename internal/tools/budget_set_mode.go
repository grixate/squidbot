package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/grixate/squidbot/internal/budget"
)

type BudgetSetModeRequest struct {
	SessionID string
	Channel   string
	SenderID  string
	Mode      string
}

type BudgetSetModeResponse struct {
	Settings budget.Settings
}

type BudgetSetModeFunc func(ctx context.Context, req BudgetSetModeRequest) (BudgetSetModeResponse, error)

type BudgetSetModeTool struct {
	set       BudgetSetModeFunc
	sessionID string
	channel   string
	senderID  string
}

func NewBudgetSetModeTool(set BudgetSetModeFunc) *BudgetSetModeTool {
	return &BudgetSetModeTool{set: set}
}

func (t *BudgetSetModeTool) SetContext(sessionID, channel, senderID string) {
	t.sessionID = sessionID
	t.channel = channel
	t.senderID = senderID
}

func (t *BudgetSetModeTool) Name() string { return "budget_set_mode" }

func (t *BudgetSetModeTool) Description() string {
	return "Update token safety mode (hybrid, soft, hard)."
}

func (t *BudgetSetModeTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"mode": map[string]any{"type": "string", "enum": []string{"hybrid", "soft", "hard"}},
		},
		"required": []string{"mode"},
	}
}

func (t *BudgetSetModeTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.set == nil {
		return ToolResult{}, fmt.Errorf("budget manager is not configured")
	}
	var in struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(in.Mode) == "" {
		return ToolResult{}, fmt.Errorf("mode is required")
	}
	out, err := t.set(ctx, BudgetSetModeRequest{
		SessionID: t.sessionID,
		Channel:   t.channel,
		SenderID:  t.senderID,
		Mode:      strings.TrimSpace(in.Mode),
	})
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{
		Text: fmt.Sprintf("Token safety mode set to %s.", out.Settings.Mode),
		Metadata: map[string]any{
			"settings": out.Settings,
		},
	}, nil
}
