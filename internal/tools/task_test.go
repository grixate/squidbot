package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCreateTaskToolValidationAndContext(t *testing.T) {
	tool := NewCreateTaskTool(nil)
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"title":"x"}`)); err == nil {
		t.Fatal("expected not configured error")
	}

	var captured CreateTaskRequest
	tool = NewCreateTaskTool(func(ctx context.Context, req CreateTaskRequest) (TaskResult, error) {
		captured = req
		return TaskResult{ID: "task-1", ColumnID: "backlog", Updated: true}, nil
	})
	tool.SetContext("session-1", "chat", "room-1", "req-1", "manual")

	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"title":" "}`)); err == nil {
		t.Fatal("expected title validation error")
	}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"title":"x","due_at":"bad"}`)); err == nil {
		t.Fatal("expected due_at validation error")
	}

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"title":"Ship","priority":"high","due_at":"2026-02-18T12:00:00Z"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Text, "task-1") || !strings.Contains(result.Text, "updated") {
		t.Fatalf("unexpected result text: %q", result.Text)
	}
	if captured.Title != "Ship" || captured.Priority != "high" {
		t.Fatalf("unexpected captured create request: %+v", captured)
	}
	if captured.SessionID != "session-1" || captured.Channel != "chat" || captured.ChatID != "room-1" || captured.RequestID != "req-1" || captured.Trigger != "manual" {
		t.Fatalf("context fields not propagated: %+v", captured)
	}
	if captured.DueAt == nil || !captured.DueAt.Equal(time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected due_at parsing: %+v", captured.DueAt)
	}
}

func TestUpdateTaskToolValidationAndContext(t *testing.T) {
	tool := NewUpdateTaskTool(nil)
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"task_id":"x"}`)); err == nil {
		t.Fatal("expected not configured error")
	}

	var captured UpdateTaskRequest
	tool = NewUpdateTaskTool(func(ctx context.Context, req UpdateTaskRequest) (TaskResult, error) {
		captured = req
		return TaskResult{ID: "task-2", ColumnID: "done"}, nil
	})
	tool.SetContext("session-2", "telegram", "chat-7", "req-2", "auto")

	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"task_id":" "}`)); err == nil {
		t.Fatal("expected task_id validation error")
	}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"task_id":"task-2","due_at":"bad"}`)); err == nil {
		t.Fatal("expected due_at validation error")
	}

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"task_id":"task-2","column_id":"done","due_at":"2026-02-18T13:00:00Z"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Text, "task-2") || !strings.Contains(result.Text, "done") {
		t.Fatalf("unexpected result text: %q", result.Text)
	}
	if captured.TaskID != "task-2" || captured.ColumnID != "done" {
		t.Fatalf("unexpected captured update request: %+v", captured)
	}
	if captured.SessionID != "session-2" || captured.Channel != "telegram" || captured.ChatID != "chat-7" || captured.RequestID != "req-2" || captured.Trigger != "auto" {
		t.Fatalf("context fields not propagated: %+v", captured)
	}
	if captured.DueAt == nil || !captured.DueAt.Equal(time.Date(2026, 2, 18, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected due_at parsing: %+v", captured.DueAt)
	}
}
