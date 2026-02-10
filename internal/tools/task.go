package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type TaskResult struct {
	ID       string
	ColumnID string
	Updated  bool
}

type CreateTaskRequest struct {
	Title       string
	Description string
	Priority    string
	Assignee    string
	Notes       string
	DueAt       *time.Time
	ColumnID    string
	SessionID   string
	Channel     string
	ChatID      string
	RequestID   string
	Trigger     string
}

type UpdateTaskRequest struct {
	TaskID      string
	Title       string
	Description string
	Priority    string
	Assignee    string
	Notes       string
	ColumnID    string
	DueAt       *time.Time
	SessionID   string
	Channel     string
	ChatID      string
	RequestID   string
	Trigger     string
}

type CreateTaskFunc func(ctx context.Context, req CreateTaskRequest) (TaskResult, error)
type UpdateTaskFunc func(ctx context.Context, req UpdateTaskRequest) (TaskResult, error)

type CreateTaskTool struct {
	create    CreateTaskFunc
	sessionID string
	channel   string
	chatID    string
	requestID string
	trigger   string
}

func NewCreateTaskTool(create CreateTaskFunc) *CreateTaskTool {
	return &CreateTaskTool{create: create}
}

func (t *CreateTaskTool) SetContext(sessionID, channel, chatID, requestID, trigger string) {
	t.sessionID = sessionID
	t.channel = channel
	t.chatID = chatID
	t.requestID = requestID
	t.trigger = trigger
}

func (t *CreateTaskTool) Name() string { return "create_task" }
func (t *CreateTaskTool) Description() string {
	return "Create or deduplicate a Mission Control task card in kanban."
}
func (t *CreateTaskTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":       map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
			"priority":    map[string]any{"type": "string", "enum": []string{"critical", "high", "medium", "low"}},
			"assignee":    map[string]any{"type": "string"},
			"notes":       map[string]any{"type": "string"},
			"column_id":   map[string]any{"type": "string"},
			"due_at":      map[string]any{"type": "string", "description": "RFC3339 timestamp"},
		},
		"required": []string{"title"},
	}
}

func (t *CreateTaskTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.create == nil {
		return ToolResult{}, fmt.Errorf("task manager is not configured")
	}
	var in struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Priority    string `json:"priority"`
		Assignee    string `json:"assignee"`
		Notes       string `json:"notes"`
		ColumnID    string `json:"column_id"`
		DueAt       string `json:"due_at"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(in.Title) == "" {
		return ToolResult{}, fmt.Errorf("title is required")
	}
	var dueAt *time.Time
	if strings.TrimSpace(in.DueAt) != "" {
		parsed, err := time.Parse(time.RFC3339, in.DueAt)
		if err != nil {
			return ToolResult{}, fmt.Errorf("invalid due_at (expected RFC3339)")
		}
		parsed = parsed.UTC()
		dueAt = &parsed
	}
	out, err := t.create(ctx, CreateTaskRequest{
		Title:       in.Title,
		Description: in.Description,
		Priority:    in.Priority,
		Assignee:    in.Assignee,
		Notes:       in.Notes,
		ColumnID:    in.ColumnID,
		DueAt:       dueAt,
		SessionID:   t.sessionID,
		Channel:     t.channel,
		ChatID:      t.chatID,
		RequestID:   t.requestID,
		Trigger:     t.trigger,
	})
	if err != nil {
		return ToolResult{}, err
	}
	status := "created"
	if out.Updated {
		status = "updated"
	}
	return ToolResult{Text: fmt.Sprintf("Task %s (%s) in %s", out.ID, status, out.ColumnID)}, nil
}

type UpdateTaskTool struct {
	update    UpdateTaskFunc
	sessionID string
	channel   string
	chatID    string
	requestID string
	trigger   string
}

func NewUpdateTaskTool(update UpdateTaskFunc) *UpdateTaskTool {
	return &UpdateTaskTool{update: update}
}

func (t *UpdateTaskTool) SetContext(sessionID, channel, chatID, requestID, trigger string) {
	t.sessionID = sessionID
	t.channel = channel
	t.chatID = chatID
	t.requestID = requestID
	t.trigger = trigger
}

func (t *UpdateTaskTool) Name() string { return "update_task" }
func (t *UpdateTaskTool) Description() string {
	return "Update an existing Mission Control task card."
}
func (t *UpdateTaskTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id":     map[string]any{"type": "string"},
			"title":       map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
			"priority":    map[string]any{"type": "string", "enum": []string{"critical", "high", "medium", "low"}},
			"assignee":    map[string]any{"type": "string"},
			"notes":       map[string]any{"type": "string"},
			"column_id":   map[string]any{"type": "string"},
			"due_at":      map[string]any{"type": "string", "description": "RFC3339 timestamp or empty string to clear"},
		},
		"required": []string{"task_id"},
	}
}

func (t *UpdateTaskTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.update == nil {
		return ToolResult{}, fmt.Errorf("task manager is not configured")
	}
	var in struct {
		TaskID      string `json:"task_id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Priority    string `json:"priority"`
		Assignee    string `json:"assignee"`
		Notes       string `json:"notes"`
		ColumnID    string `json:"column_id"`
		DueAt       string `json:"due_at"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(in.TaskID) == "" {
		return ToolResult{}, fmt.Errorf("task_id is required")
	}
	var dueAt *time.Time
	if strings.TrimSpace(in.DueAt) != "" {
		parsed, err := time.Parse(time.RFC3339, in.DueAt)
		if err != nil {
			return ToolResult{}, fmt.Errorf("invalid due_at (expected RFC3339)")
		}
		parsed = parsed.UTC()
		dueAt = &parsed
	}
	out, err := t.update(ctx, UpdateTaskRequest{
		TaskID:      in.TaskID,
		Title:       in.Title,
		Description: in.Description,
		Priority:    in.Priority,
		Assignee:    in.Assignee,
		Notes:       in.Notes,
		ColumnID:    in.ColumnID,
		DueAt:       dueAt,
		SessionID:   t.sessionID,
		Channel:     t.channel,
		ChatID:      t.chatID,
		RequestID:   t.requestID,
		Trigger:     t.trigger,
	})
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Text: fmt.Sprintf("Task %s updated in %s", out.ID, out.ColumnID)}, nil
}
