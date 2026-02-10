package mission

import (
	"strings"
	"time"
	"unicode"
)

type TaskSourceType string

const (
	TaskSourceManual    TaskSourceType = "manual"
	TaskSourceChat      TaskSourceType = "chat"
	TaskSourceHeartbeat TaskSourceType = "heartbeat"
	TaskSourceCron      TaskSourceType = "cron"
	TaskSourceSubagent  TaskSourceType = "subagent"
	TaskSourceSystem    TaskSourceType = "system"
	TaskSourceAPI       TaskSourceType = "api"
)

type TaskSource struct {
	Type      TaskSourceType `json:"type"`
	SessionID string         `json:"session_id,omitempty"`
	Channel   string         `json:"channel,omitempty"`
	ChatID    string         `json:"chat_id,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
	Trigger   string         `json:"trigger,omitempty"`
}

type TaskEventType string

const (
	TaskEventCreated TaskEventType = "created"
	TaskEventUpdated TaskEventType = "updated"
	TaskEventMoved   TaskEventType = "moved"
	TaskEventDeleted TaskEventType = "deleted"
)

type TaskEvent struct {
	ID        string         `json:"id"`
	Type      TaskEventType  `json:"type"`
	Actor     string         `json:"actor,omitempty"`
	Summary   string         `json:"summary,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type Task struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Description string      `json:"description,omitempty"`
	ColumnID    string      `json:"column_id"`
	Priority    string      `json:"priority,omitempty"`
	Assignee    string      `json:"assignee,omitempty"`
	Notes       string      `json:"notes,omitempty"`
	DueAt       *time.Time  `json:"due_at,omitempty"`
	Source      TaskSource  `json:"source"`
	Position    int         `json:"position"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	Version     int         `json:"version"`
	Events      []TaskEvent `json:"events,omitempty"`
}

type Column struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Position  int       `json:"position"`
	System    bool      `json:"system"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Version   int       `json:"version"`
}

const (
	ColumnBacklog    = "backlog"
	ColumnInProgress = "in_progress"
	ColumnBlocked    = "blocked"
	ColumnDone       = "done"
)

func DefaultColumns(now time.Time) []Column {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return []Column{
		{ID: ColumnBacklog, Label: "Backlog", Position: 0, System: true, CreatedAt: now, UpdatedAt: now, Version: 1},
		{ID: ColumnInProgress, Label: "In Progress", Position: 1, System: true, CreatedAt: now, UpdatedAt: now, Version: 1},
		{ID: ColumnBlocked, Label: "Blocked", Position: 2, System: true, CreatedAt: now, UpdatedAt: now, Version: 1},
		{ID: ColumnDone, Label: "Done", Position: 3, System: true, CreatedAt: now, UpdatedAt: now, Version: 1},
	}
}

type UsageDay struct {
	Day              string    `json:"day"`
	PromptTokens     uint64    `json:"prompt_tokens"`
	CompletionTokens uint64    `json:"completion_tokens"`
	TotalTokens      uint64    `json:"total_tokens"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type HeartbeatRun struct {
	ID          string    `json:"id"`
	TriggeredBy string    `json:"triggered_by"`
	Status      string    `json:"status"`
	Error       string    `json:"error,omitempty"`
	Preview     string    `json:"preview,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	DurationMS  int64     `json:"duration_ms"`
}

type TaskAutomationPolicy struct {
	EnableChat      bool      `json:"enable_chat"`
	EnableHeartbeat bool      `json:"enable_heartbeat"`
	EnableCron      bool      `json:"enable_cron"`
	EnableSubagent  bool      `json:"enable_subagent"`
	DedupeWindowSec int       `json:"dedupe_window_sec"`
	DefaultColumnID string    `json:"default_column_id"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func DefaultTaskAutomationPolicy(now time.Time) TaskAutomationPolicy {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return TaskAutomationPolicy{
		EnableChat:      true,
		EnableHeartbeat: true,
		EnableCron:      true,
		EnableSubagent:  true,
		DedupeWindowSec: int((6 * time.Hour).Seconds()),
		DefaultColumnID: ColumnBacklog,
		UpdatedAt:       now,
	}
}

func (p TaskAutomationPolicy) DedupeWindow() time.Duration {
	sec := p.DedupeWindowSec
	if sec <= 0 {
		sec = int((6 * time.Hour).Seconds())
	}
	return time.Duration(sec) * time.Second
}

func (p TaskAutomationPolicy) EnabledForSource(source TaskSourceType) bool {
	switch source {
	case TaskSourceHeartbeat:
		return p.EnableHeartbeat
	case TaskSourceCron:
		return p.EnableCron
	case TaskSourceSubagent:
		return p.EnableSubagent
	case TaskSourceChat:
		return p.EnableChat
	default:
		return true
	}
}

func NormalizeTaskTitle(in string) string {
	trimmed := strings.TrimSpace(strings.ToLower(in))
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(trimmed))
	space := false
	for _, r := range trimmed {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
			continue
		}
		if !space {
			b.WriteRune(' ')
		}
		space = true
	}
	return strings.TrimSpace(b.String())
}

func NormalizePriority(in string) string {
	switch strings.TrimSpace(strings.ToLower(in)) {
	case "critical":
		return "critical"
	case "high":
		return "high"
	case "medium":
		return "medium"
	case "low":
		return "low"
	default:
		return ""
	}
}
