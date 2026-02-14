package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/grixate/squidbot/internal/budget"
)

type BudgetSetEstimationRequest struct {
	SessionID              string
	Channel                string
	SenderID               string
	EstimateOnMissingUsage *bool
	EstimateCharsPerToken  *int
}

type BudgetSetEstimationResponse struct {
	Settings budget.Settings
}

type BudgetSetEstimationFunc func(ctx context.Context, req BudgetSetEstimationRequest) (BudgetSetEstimationResponse, error)

type BudgetSetEstimationTool struct {
	set       BudgetSetEstimationFunc
	sessionID string
	channel   string
	senderID  string
}

func NewBudgetSetEstimationTool(set BudgetSetEstimationFunc) *BudgetSetEstimationTool {
	return &BudgetSetEstimationTool{set: set}
}

func (t *BudgetSetEstimationTool) SetContext(sessionID, channel, senderID string) {
	t.sessionID = sessionID
	t.channel = channel
	t.senderID = senderID
}

func (t *BudgetSetEstimationTool) Name() string { return "budget_set_estimation" }

func (t *BudgetSetEstimationTool) Description() string {
	return "Update missing-usage estimation settings for token safety."
}

func (t *BudgetSetEstimationTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"estimate_on_missing_usage": map[string]any{"type": "boolean"},
			"estimate_chars_per_token":  map[string]any{"type": "integer"},
		},
	}
}

func (t *BudgetSetEstimationTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.set == nil {
		return ToolResult{}, fmt.Errorf("budget manager is not configured")
	}
	var in struct {
		EstimateOnMissingUsage *bool `json:"estimate_on_missing_usage"`
		EstimateCharsPerToken  *int  `json:"estimate_chars_per_token"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	out, err := t.set(ctx, BudgetSetEstimationRequest{
		SessionID:              t.sessionID,
		Channel:                t.channel,
		SenderID:               t.senderID,
		EstimateOnMissingUsage: in.EstimateOnMissingUsage,
		EstimateCharsPerToken:  in.EstimateCharsPerToken,
	})
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{
		Text: "Token safety estimation settings updated.",
		Metadata: map[string]any{
			"settings": out.Settings,
		},
	}, nil
}
