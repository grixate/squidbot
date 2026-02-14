package subagent

import (
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/provider"
)

type Status string

const (
	CancelSignalNamespace = "subagent_cancel"

	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusTimedOut  Status = "timed_out"
	StatusCancelled Status = "cancelled"
)

func (s Status) Terminal() bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusTimedOut, StatusCancelled:
		return true
	default:
		return false
	}
}

type ContextMode string

const (
	ContextModeMinimal       ContextMode = "minimal"
	ContextModeSession       ContextMode = "session"
	ContextModeSessionMemory ContextMode = "session_memory"
)

func NormalizeContextMode(in string) ContextMode {
	switch strings.TrimSpace(strings.ToLower(in)) {
	case string(ContextModeSession):
		return ContextModeSession
	case string(ContextModeSessionMemory):
		return ContextModeSessionMemory
	default:
		return ContextModeMinimal
	}
}

type ContextPacket struct {
	Mode           ContextMode        `json:"mode"`
	SystemPrompt   string             `json:"system_prompt,omitempty"`
	History        []provider.Message `json:"history,omitempty"`
	MemorySnippets []string           `json:"memory_snippets,omitempty"`
	Attachments    []string           `json:"attachments,omitempty"`
	CreatedAt      time.Time          `json:"created_at"`
	Checksum       string             `json:"checksum,omitempty"`
}

type Result struct {
	Summary       string   `json:"summary,omitempty"`
	Output        string   `json:"output,omitempty"`
	ArtifactPaths []string `json:"artifact_paths,omitempty"`
}

type Run struct {
	ID               string        `json:"id"`
	SessionID        string        `json:"session_id"`
	Channel          string        `json:"channel"`
	ChatID           string        `json:"chat_id"`
	SenderID         string        `json:"sender_id,omitempty"`
	Label            string        `json:"label,omitempty"`
	Task             string        `json:"task"`
	Status           Status        `json:"status"`
	Error            string        `json:"error,omitempty"`
	CreatedAt        time.Time     `json:"created_at"`
	StartedAt        *time.Time    `json:"started_at,omitempty"`
	FinishedAt       *time.Time    `json:"finished_at,omitempty"`
	TimeoutSec       int           `json:"timeout_sec"`
	MaxAttempts      int           `json:"max_attempts"`
	Attempt          int           `json:"attempt"`
	Depth            int           `json:"depth"`
	NotifyOnComplete bool          `json:"notify_on_complete"`
	ArtifactDir      string        `json:"artifact_dir,omitempty"`
	Context          ContextPacket `json:"context"`
	Result           *Result       `json:"result,omitempty"`
}

type Event struct {
	ID        string    `json:"id"`
	RunID     string    `json:"run_id"`
	Status    Status    `json:"status"`
	Message   string    `json:"message,omitempty"`
	Attempt   int       `json:"attempt"`
	CreatedAt time.Time `json:"created_at"`
}

type Request struct {
	ID               string
	SessionID        string
	Channel          string
	ChatID           string
	SenderID         string
	Task             string
	Label            string
	ContextMode      ContextMode
	Attachments      []string
	TimeoutSec       int
	MaxAttempts      int
	Depth            int
	NotifyOnComplete bool
	ArtifactDir      string
	Context          ContextPacket
}
