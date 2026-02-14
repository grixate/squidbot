package budget

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type Mode string

const (
	ModeHybrid Mode = "hybrid"
	ModeSoft   Mode = "soft"
	ModeHard   Mode = "hard"
)

var ErrNotFound = errors.New("budget record not found")

type Settings struct {
	Enabled                     bool     `json:"enabled"`
	Mode                        Mode     `json:"mode"`
	GlobalHardLimitTokens       uint64   `json:"global_hard_limit_tokens"`
	GlobalSoftThresholdPct      int      `json:"global_soft_threshold_pct"`
	SessionHardLimitTokens      uint64   `json:"session_hard_limit_tokens"`
	SessionSoftThresholdPct     int      `json:"session_soft_threshold_pct"`
	SubagentRunHardLimitTokens  uint64   `json:"subagent_run_hard_limit_tokens"`
	SubagentRunSoftThresholdPct int      `json:"subagent_run_soft_threshold_pct"`
	EstimateOnMissingUsage      bool     `json:"estimate_on_missing_usage"`
	EstimateCharsPerToken       int      `json:"estimate_chars_per_token"`
	TrustedWriters              []string `json:"trusted_writers,omitempty"`
	ReservationTTLSec           int      `json:"reservation_ttl_sec"`
}

func (s Settings) Normalized() Settings {
	out := s
	mode := NormalizeMode(string(out.Mode))
	out.Mode = Mode(mode)
	if out.GlobalSoftThresholdPct < 0 {
		out.GlobalSoftThresholdPct = 0
	}
	if out.GlobalSoftThresholdPct > 100 {
		out.GlobalSoftThresholdPct = 100
	}
	if out.SessionSoftThresholdPct < 0 {
		out.SessionSoftThresholdPct = 0
	}
	if out.SessionSoftThresholdPct > 100 {
		out.SessionSoftThresholdPct = 100
	}
	if out.SubagentRunSoftThresholdPct < 0 {
		out.SubagentRunSoftThresholdPct = 0
	}
	if out.SubagentRunSoftThresholdPct > 100 {
		out.SubagentRunSoftThresholdPct = 100
	}
	if out.EstimateCharsPerToken <= 0 {
		out.EstimateCharsPerToken = 4
	}
	if out.ReservationTTLSec <= 0 {
		out.ReservationTTLSec = 300
	}
	trimmed := make([]string, 0, len(out.TrustedWriters))
	for _, writer := range out.TrustedWriters {
		normalized := strings.ToLower(strings.TrimSpace(writer))
		if normalized == "" {
			continue
		}
		trimmed = append(trimmed, normalized)
	}
	out.TrustedWriters = trimmed
	return out
}

func NormalizeMode(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case string(ModeSoft):
		return string(ModeSoft)
	case string(ModeHard):
		return string(ModeHard)
	default:
		return string(ModeHybrid)
	}
}

type TokenSafetyOverride struct {
	Settings  Settings  `json:"settings"`
	UpdatedAt time.Time `json:"updated_at"`
	Version   int       `json:"version"`
}

type Counter struct {
	Scope            string    `json:"scope"`
	PromptTokens     uint64    `json:"prompt_tokens"`
	CompletionTokens uint64    `json:"completion_tokens"`
	TotalTokens      uint64    `json:"total_tokens"`
	ReservedTokens   uint64    `json:"reserved_tokens"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Reservation struct {
	ID          string    `json:"id"`
	Scope       string    `json:"scope"`
	Tokens      uint64    `json:"tokens"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	Finalized   bool      `json:"finalized"`
	Cancelled   bool      `json:"cancelled"`
	FinalizedAt time.Time `json:"finalized_at,omitempty"`
}

type ScopeLimit struct {
	Key              string
	HardLimitTokens  uint64
	SoftThresholdPct int
}

type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	OutputChars      int
}

type Warning struct {
	Scope        string `json:"scope"`
	Used         uint64 `json:"used"`
	Reserved     uint64 `json:"reserved"`
	HardLimit    uint64 `json:"hard_limit"`
	ThresholdPct int    `json:"threshold_pct"`
}

type PreflightResult struct {
	Reservations map[string]string
	Warnings     []Warning
}

type CommitResult struct {
	Warnings        []Warning
	Estimated       bool
	EstimatedTokens uint64
	TotalTokens     uint64
}

type LimitError struct {
	Scope     string
	Used      uint64
	Reserved  uint64
	Requested uint64
	Limit     uint64
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("token limit exceeded for %s: used=%d reserved=%d requested=%d limit=%d", e.Scope, e.Used, e.Reserved, e.Requested, e.Limit)
}
