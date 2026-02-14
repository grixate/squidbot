package telemetry

import (
	"sync/atomic"
)

type Metrics struct {
	InboundCount        atomic.Uint64
	OutboundCount       atomic.Uint64
	ActiveActors        atomic.Int64
	ActiveTurns         atomic.Int64
	ProviderCalls       atomic.Uint64
	ProviderErrors      atomic.Uint64
	ToolCalls           atomic.Uint64
	ToolErrors          atomic.Uint64
	CronExecutions      atomic.Uint64
	HeartbeatExecutions atomic.Uint64
	SubagentQueued      atomic.Uint64
	SubagentRunning     atomic.Uint64
	SubagentSucceeded   atomic.Uint64
	SubagentFailed      atomic.Uint64
	SubagentTimedOut    atomic.Uint64
	SubagentCancelled   atomic.Uint64
	SubagentRetries     atomic.Uint64
	SubagentQueueDepth  atomic.Uint64
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
		"inbound_count":        m.InboundCount.Load(),
		"outbound_count":       m.OutboundCount.Load(),
		"active_actors":        uint64(active),
		"active_turns":         uint64(turns),
		"provider_calls":       m.ProviderCalls.Load(),
		"provider_errors":      m.ProviderErrors.Load(),
		"tool_calls":           m.ToolCalls.Load(),
		"tool_errors":          m.ToolErrors.Load(),
		"cron_executions":      m.CronExecutions.Load(),
		"heartbeat_executions": m.HeartbeatExecutions.Load(),
		"subagent_queued":      m.SubagentQueued.Load(),
		"subagent_running":     m.SubagentRunning.Load(),
		"subagent_succeeded":   m.SubagentSucceeded.Load(),
		"subagent_failed":      m.SubagentFailed.Load(),
		"subagent_timed_out":   m.SubagentTimedOut.Load(),
		"subagent_cancelled":   m.SubagentCancelled.Load(),
		"subagent_retries":     m.SubagentRetries.Load(),
		"subagent_queue_depth": m.SubagentQueueDepth.Load(),
	}
}
