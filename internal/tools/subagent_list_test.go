package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/subagent"
)

func TestSubagentListToolPassesFiltersAndSession(t *testing.T) {
	var got SubagentListRequest
	tool := NewSubagentListTool(func(ctx context.Context, req SubagentListRequest) (SubagentListResponse, error) {
		got = req
		return SubagentListResponse{Runs: []subagent.Run{{
			ID:        "run-1",
			Status:    subagent.StatusRunning,
			Task:      "analyze",
			CreatedAt: time.Now().UTC(),
		}}}, nil
	})
	tool.SetContext("cli:default")

	args, _ := json.Marshal(map[string]any{
		"status": "active",
		"limit":  5,
	})
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != "cli:default" {
		t.Fatalf("unexpected session id: %q", got.SessionID)
	}
	if got.Status != "active" || got.Limit != 5 {
		t.Fatalf("unexpected request: %+v", got)
	}
	if result.Text == "" {
		t.Fatal("expected non-empty text")
	}
	runs, ok := result.Metadata["runs"].([]map[string]any)
	if !ok || len(runs) != 1 {
		t.Fatalf("expected one run in metadata, got %#v", result.Metadata["runs"])
	}
}

func TestSubagentListToolEmptyResult(t *testing.T) {
	tool := NewSubagentListTool(func(ctx context.Context, req SubagentListRequest) (SubagentListResponse, error) {
		return SubagentListResponse{}, nil
	})
	tool.SetContext("cli:default")

	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "No subagent runs found." {
		t.Fatalf("unexpected text: %q", result.Text)
	}
}
