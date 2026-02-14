package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/grixate/squidbot/internal/subagent"
)

func TestSpawnToolBackwardCompatibleTaskOnly(t *testing.T) {
	tool := NewSpawnTool(func(ctx context.Context, req SpawnRequest) (SpawnResponse, error) {
		return SpawnResponse{RunID: "run-1", Status: subagent.StatusQueued, Text: "queued"}, nil
	})
	tool.SetContext("cli:default", "cli", "direct", "user", 0)
	args, _ := json.Marshal(map[string]any{"task": "hello"})
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "queued" {
		t.Fatalf("unexpected text: %q", result.Text)
	}
	runID, _ := result.Metadata["run_id"].(string)
	if runID != "run-1" {
		t.Fatalf("unexpected run id: %q", runID)
	}
}

func TestSpawnToolParsesExtendedFields(t *testing.T) {
	var got SpawnRequest
	tool := NewSpawnTool(func(ctx context.Context, req SpawnRequest) (SpawnResponse, error) {
		got = req
		return SpawnResponse{RunID: "run-2", Status: subagent.StatusSucceeded, Text: "done"}, nil
	})
	tool.SetContext("s1", "telegram", "c1", "u1", 2)
	args, _ := json.Marshal(map[string]any{
		"task":         "analyze",
		"label":        "analysis",
		"context_mode": "session_memory",
		"attachments":  []string{"README.md"},
		"timeout_sec":  10,
		"max_attempts": 3,
		"wait":         true,
	})
	_, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if got.ContextMode != subagent.ContextModeSessionMemory {
		t.Fatalf("unexpected context mode: %s", got.ContextMode)
	}
	if got.TimeoutSec != 10 || got.MaxAttempts != 3 || !got.Wait {
		t.Fatalf("unexpected wait args: %+v", got)
	}
	if got.Depth != 2 {
		t.Fatalf("unexpected depth: %d", got.Depth)
	}
}
