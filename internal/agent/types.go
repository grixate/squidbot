package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/grixate/squidbot/internal/budget"
	"github.com/grixate/squidbot/internal/mission"
	"github.com/grixate/squidbot/internal/provider"
	"github.com/grixate/squidbot/internal/subagent"
)

type InboundMessage struct {
	RequestID string         `json:"request_id"`
	SessionID string         `json:"session_id"`
	Channel   string         `json:"channel"`
	ChatID    string         `json:"chat_id"`
	SenderID  string         `json:"sender_id"`
	Content   string         `json:"content"`
	Media     []string       `json:"media,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type OutboundMessage struct {
	Channel  string         `json:"channel"`
	ChatID   string         `json:"chat_id"`
	Content  string         `json:"content"`
	ReplyTo  string         `json:"reply_to,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type StreamEvent struct {
	Type       string         `json:"type"`
	Delta      string         `json:"delta,omitempty"`
	Content    string         `json:"content,omitempty"`
	ToolName   string         `json:"tool_name,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Error      string         `json:"error,omitempty"`
	Done       bool           `json:"done,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type StreamSink interface {
	OnEvent(ctx context.Context, event StreamEvent) error
}

type StreamSinkFunc func(ctx context.Context, event StreamEvent) error

func (f StreamSinkFunc) OnEvent(ctx context.Context, event StreamEvent) error {
	return f(ctx, event)
}

type OutboundStream struct {
	Channel  string         `json:"channel"`
	ChatID   string         `json:"chat_id"`
	ReplyTo  string         `json:"reply_to,omitempty"`
	Events   []StreamEvent  `json:"events"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type Ack struct {
	RequestID string `json:"request_id"`
}

type SessionSnapshot struct {
	SessionID string             `json:"session_id"`
	Messages  []provider.Message `json:"messages"`
}

type SessionEngine interface {
	Submit(ctx context.Context, msg InboundMessage) (Ack, error)
	Snapshot(ctx context.Context, sessionID string) (SessionSnapshot, error)
}

type Turn struct {
	ID         string              `json:"id"`
	SessionID  string              `json:"session_id"`
	Role       string              `json:"role"`
	Content    string              `json:"content"`
	Name       string              `json:"name,omitempty"`
	ToolCalls  []provider.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
	Metadata   json.RawMessage     `json:"metadata,omitempty"`
	CreatedAt  time.Time           `json:"created_at"`
	Version    int                 `json:"version"`
}

type ToolEvent struct {
	ID        string          `json:"id"`
	SessionID string          `json:"session_id"`
	ToolName  string          `json:"tool_name"`
	Input     string          `json:"input"`
	Output    string          `json:"output"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	Version   int             `json:"version"`
}

type ConversationStore interface {
	AppendTurn(ctx context.Context, turn Turn) error
	Window(ctx context.Context, sessionID string, limit int) ([]provider.Message, error)
	SaveSessionMeta(ctx context.Context, sessionID string, meta map[string]any) error
}

type KVStore interface {
	PutKV(ctx context.Context, namespace, key string, value []byte) error
	GetKV(ctx context.Context, namespace, key string) ([]byte, error)
}

type SchedulerStore interface {
	PutJob(ctx context.Context, job []byte, id string) error
	DeleteJob(ctx context.Context, id string) error
	ListJobs(ctx context.Context) (map[string][]byte, error)
	RecordJobRun(ctx context.Context, runID string, payload []byte) error
}

type ToolEventStore interface {
	AppendToolEvent(ctx context.Context, event ToolEvent) error
}

type MissionStore interface {
	PutMissionTask(ctx context.Context, task mission.Task) error
	DeleteMissionTask(ctx context.Context, id string) error
	ListMissionTasks(ctx context.Context) ([]mission.Task, error)
	ReplaceMissionColumns(ctx context.Context, columns []mission.Column) error
	ListMissionColumns(ctx context.Context) ([]mission.Column, error)
	RecordUsageDay(ctx context.Context, day string, promptTokens, completionTokens, totalTokens uint64) error
	GetTaskAutomationPolicy(ctx context.Context) (mission.TaskAutomationPolicy, error)
}

type SubagentStore interface {
	PutSubagentRun(ctx context.Context, run subagent.Run) error
	GetSubagentRun(ctx context.Context, id string) (subagent.Run, error)
	ListSubagentRunsBySession(ctx context.Context, sessionID string, limit int) ([]subagent.Run, error)
	ListSubagentRunsByStatus(ctx context.Context, status subagent.Status, limit int) ([]subagent.Run, error)
	AppendSubagentEvent(ctx context.Context, event subagent.Event) error
}

type BudgetStore interface {
	GetTokenSafetyOverride(ctx context.Context) (budget.TokenSafetyOverride, error)
	PutTokenSafetyOverride(ctx context.Context, override budget.TokenSafetyOverride) error
	GetBudgetCounter(ctx context.Context, scope string) (budget.Counter, error)
	AddBudgetUsage(ctx context.Context, scope string, prompt, completion, total uint64) error
	ReserveBudget(ctx context.Context, scope string, tokens uint64, ttlSec int) (reservationID string, err error)
	FinalizeBudgetReservation(ctx context.Context, reservationID string, actualTotal uint64) error
	CancelBudgetReservation(ctx context.Context, reservationID string) error
	ListBudgetReservations(ctx context.Context, scope string, limit int) ([]budget.Reservation, error)
}
