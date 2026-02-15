package telemetry

import (
	"sync/atomic"
)

type Metrics struct {
	InboundCount                atomic.Uint64
	OutboundCount               atomic.Uint64
	ActiveActors                atomic.Int64
	ActiveTurns                 atomic.Int64
	ProviderCalls               atomic.Uint64
	ProviderErrors              atomic.Uint64
	ToolCalls                   atomic.Uint64
	ToolErrors                  atomic.Uint64
	CronExecutions              atomic.Uint64
	HeartbeatExecutions         atomic.Uint64
	SubagentQueued              atomic.Uint64
	SubagentRunning             atomic.Uint64
	SubagentSucceeded           atomic.Uint64
	SubagentFailed              atomic.Uint64
	SubagentTimedOut            atomic.Uint64
	SubagentCancelled           atomic.Uint64
	SubagentRetries             atomic.Uint64
	SubagentQueueDepth          atomic.Uint64
	DelegationsSubmitted        atomic.Uint64
	DelegationsSucceeded        atomic.Uint64
	DelegationsFailed           atomic.Uint64
	DelegationLatencyMS         atomic.Uint64
	PeerHealthState             atomic.Uint64
	FallbackCount               atomic.Uint64
	IdempotencyHits             atomic.Uint64
	TokenSafetyPreflightAllowed atomic.Uint64
	TokenSafetyPreflightBlocked atomic.Uint64
	TokenSafetySoftWarnings     atomic.Uint64
	TokenSafetyEstimatedUsage   atomic.Uint64
	TokenSafetyDisabledBypass   atomic.Uint64
	SkillsRouterRuns            atomic.Uint64
	SkillsActivatedTotal        atomic.Uint64
	SkillsExplicitFailures      atomic.Uint64
	SkillsInvalidSkipped        atomic.Uint64
	SkillsReloadTotal           atomic.Uint64
}

func (m *Metrics) Snapshot() map[string]uint64 {
	active := m.ActiveActors.Load()
	if active < 0 {
		active = 0
	}
	turns := m.ActiveTurns.Load()
	if turns < 0 {
		turns = 0
	}
	return map[string]uint64{
		"inbound_count":                  m.InboundCount.Load(),
		"outbound_count":                 m.OutboundCount.Load(),
		"active_actors":                  uint64(active),
		"active_turns":                   uint64(turns),
		"provider_calls":                 m.ProviderCalls.Load(),
		"provider_errors":                m.ProviderErrors.Load(),
		"tool_calls":                     m.ToolCalls.Load(),
		"tool_errors":                    m.ToolErrors.Load(),
		"cron_executions":                m.CronExecutions.Load(),
		"heartbeat_executions":           m.HeartbeatExecutions.Load(),
		"subagent_queued":                m.SubagentQueued.Load(),
		"subagent_running":               m.SubagentRunning.Load(),
		"subagent_succeeded":             m.SubagentSucceeded.Load(),
		"subagent_failed":                m.SubagentFailed.Load(),
		"subagent_timed_out":             m.SubagentTimedOut.Load(),
		"subagent_cancelled":             m.SubagentCancelled.Load(),
		"subagent_retries":               m.SubagentRetries.Load(),
		"subagent_queue_depth":           m.SubagentQueueDepth.Load(),
		"delegations_submitted_total":    m.DelegationsSubmitted.Load(),
		"delegations_succeeded_total":    m.DelegationsSucceeded.Load(),
		"delegations_failed_total":       m.DelegationsFailed.Load(),
		"delegation_latency_ms":          m.DelegationLatencyMS.Load(),
		"peer_health_state":              m.PeerHealthState.Load(),
		"fallback_count_total":           m.FallbackCount.Load(),
		"idempotency_hits_total":         m.IdempotencyHits.Load(),
		"token_safety_preflight_allowed": m.TokenSafetyPreflightAllowed.Load(),
		"token_safety_preflight_blocked": m.TokenSafetyPreflightBlocked.Load(),
		"token_safety_soft_warnings":     m.TokenSafetySoftWarnings.Load(),
		"token_safety_estimated_usage":   m.TokenSafetyEstimatedUsage.Load(),
		"token_safety_disabled_bypass":   m.TokenSafetyDisabledBypass.Load(),
		"skills_router_runs":             m.SkillsRouterRuns.Load(),
		"skills_activated_total":         m.SkillsActivatedTotal.Load(),
		"skills_explicit_failures":       m.SkillsExplicitFailures.Load(),
		"skills_invalid_skipped":         m.SkillsInvalidSkipped.Load(),
		"skills_reload_total":            m.SkillsReloadTotal.Load(),
	}
}
