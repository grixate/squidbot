package federation

import (
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/provider"
)

type DelegationStatus string

const (
	StatusQueued    DelegationStatus = "queued"
	StatusRunning   DelegationStatus = "running"
	StatusSucceeded DelegationStatus = "succeeded"
	StatusFailed    DelegationStatus = "failed"
	StatusTimedOut  DelegationStatus = "timed_out"
	StatusCancelled DelegationStatus = "cancelled"
)

func (s DelegationStatus) Terminal() bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusTimedOut, StatusCancelled:
		return true
	default:
		return false
	}
}

type ContextPacket struct {
	Mode           string             `json:"mode"`
	SystemPrompt   string             `json:"system_prompt,omitempty"`
	History        []provider.Message `json:"history,omitempty"`
	MemorySnippets []string           `json:"memory_snippets,omitempty"`
	Attachments    []string           `json:"attachments,omitempty"`
	CreatedAt      time.Time          `json:"created_at"`
	Checksum       string             `json:"checksum,omitempty"`
}

type DelegationRequest struct {
	ID                   string         `json:"id,omitempty"`
	Task                 string         `json:"task"`
	Label                string         `json:"label,omitempty"`
	SessionID            string         `json:"session_id,omitempty"`
	Channel              string         `json:"channel,omitempty"`
	ChatID               string         `json:"chat_id,omitempty"`
	SenderID             string         `json:"sender_id,omitempty"`
	TimeoutSec           int            `json:"timeout_sec,omitempty"`
	MaxAttempts          int            `json:"max_attempts,omitempty"`
	Depth                int            `json:"depth,omitempty"`
	Context              ContextPacket  `json:"context"`
	Metadata             map[string]any `json:"metadata,omitempty"`
	RequiredCapabilities []string       `json:"required_capabilities,omitempty"`
	PreferredRoles       []string       `json:"preferred_roles,omitempty"`
}

type DelegationResult struct {
	Summary       string   `json:"summary,omitempty"`
	Output        string   `json:"output,omitempty"`
	ArtifactPaths []string `json:"artifact_paths,omitempty"`
}

type DeliveryAttempt struct {
	PeerID      string    `json:"peer_id"`
	Attempt     int       `json:"attempt"`
	StatusCode  int       `json:"status_code,omitempty"`
	Retryable   bool      `json:"retryable"`
	Error       string    `json:"error,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	DurationMS  int64     `json:"duration_ms"`
	Idempotency string    `json:"idempotency_key,omitempty"`
}

type RouteDecision struct {
	SelectedPeerID   string   `json:"selected_peer_id,omitempty"`
	CandidatePeerIDs []string `json:"candidate_peer_ids,omitempty"`
	Reason           string   `json:"reason,omitempty"`
}

type DelegationRun struct {
	ID              string            `json:"id"`
	OriginNodeID    string            `json:"origin_node_id,omitempty"`
	IdempotencyKey  string            `json:"idempotency_key,omitempty"`
	SessionID       string            `json:"session_id,omitempty"`
	Channel         string            `json:"channel,omitempty"`
	ChatID          string            `json:"chat_id,omitempty"`
	SenderID        string            `json:"sender_id,omitempty"`
	Task            string            `json:"task"`
	Label           string            `json:"label,omitempty"`
	Status          DelegationStatus  `json:"status"`
	Error           string            `json:"error,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	StartedAt       *time.Time        `json:"started_at,omitempty"`
	FinishedAt      *time.Time        `json:"finished_at,omitempty"`
	TimeoutSec      int               `json:"timeout_sec"`
	MaxAttempts     int               `json:"max_attempts"`
	Attempt         int               `json:"attempt"`
	Depth           int               `json:"depth"`
	PeerID          string            `json:"peer_id,omitempty"`
	RemoteRunID     string            `json:"remote_run_id,omitempty"`
	RequiredCaps    []string          `json:"required_capabilities,omitempty"`
	PreferredRoles  []string          `json:"preferred_roles,omitempty"`
	AllowFallback   bool              `json:"allow_fallback"`
	FallbackChain   []string          `json:"fallback_chain,omitempty"`
	Route           RouteDecision     `json:"route,omitempty"`
	DeliveryAttempt []DeliveryAttempt `json:"delivery_attempts,omitempty"`
	Context         ContextPacket     `json:"context"`
	Result          *DelegationResult `json:"result,omitempty"`
	Metadata        map[string]any    `json:"metadata,omitempty"`
}

type Event struct {
	ID        string           `json:"id"`
	RunID     string           `json:"run_id"`
	Status    DelegationStatus `json:"status"`
	Message   string           `json:"message,omitempty"`
	CreatedAt time.Time        `json:"created_at"`
}

type IdempotencyRecord struct {
	OriginNodeID   string    `json:"origin_node_id"`
	IdempotencyKey string    `json:"idempotency_key"`
	RunID          string    `json:"run_id"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type PeerHealth struct {
	PeerID       string    `json:"peer_id"`
	Available    bool      `json:"available"`
	QueueDepth   int       `json:"queue_depth"`
	MaxQueue     int       `json:"max_queue"`
	ActiveRuns   int       `json:"active_runs"`
	Error        string    `json:"error,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
	ResponseTime int64     `json:"response_time_ms,omitempty"`
}

func NormalizeCapabilityList(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, item := range in {
		trimmed := strings.ToLower(strings.TrimSpace(item))
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

