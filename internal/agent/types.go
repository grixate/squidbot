package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/grixate/squidbot/internal/provider"
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
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	ToolName  string    `json:"tool_name"`
	Input     string    `json:"input"`
	Output    string    `json:"output"`
	CreatedAt time.Time `json:"created_at"`
	Version   int       `json:"version"`
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
