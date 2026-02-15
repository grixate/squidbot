package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type FederationPeerInfo struct {
	ID            string
	Enabled       bool
	BaseURL       string
	Capabilities  []string
	Roles         []string
	Priority      int
	MaxConcurrent int
	MaxQueue      int
	Available     bool
	QueueDepth    int
	ActiveRuns    int
	LastError     string
	UpdatedAt     time.Time
}

type FederationPeersRequest struct{}

type FederationPeersResponse struct {
	Peers []FederationPeerInfo
}

type FederationPeersFunc func(ctx context.Context, req FederationPeersRequest) (FederationPeersResponse, error)

type FederationPeersTool struct {
	list FederationPeersFunc
}

func NewFederationPeersTool(list FederationPeersFunc) *FederationPeersTool {
	return &FederationPeersTool{list: list}
}

func (t *FederationPeersTool) Name() string { return "federation_peers" }

func (t *FederationPeersTool) Description() string {
	return "List configured federation peers and their latest health/capability snapshots."
}

func (t *FederationPeersTool) Schema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *FederationPeersTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.list == nil {
		return ToolResult{}, fmt.Errorf("federation is not configured")
	}
	if len(args) > 0 {
		var discard map[string]any
		if err := json.Unmarshal(args, &discard); err != nil {
			return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
		}
	}
	out, err := t.list(ctx, FederationPeersRequest{})
	if err != nil {
		return ToolResult{}, err
	}
	lines := make([]string, 0, len(out.Peers))
	metaPeers := make([]map[string]any, 0, len(out.Peers))
	for _, peer := range out.Peers {
		state := "unknown"
		if peer.Available {
			state = "available"
		} else if peer.LastError != "" {
			state = "degraded"
		}
		lines = append(lines, fmt.Sprintf("%s (%s) caps=%s roles=%s q=%d/%d active=%d",
			peer.ID,
			state,
			strings.Join(peer.Capabilities, ","),
			strings.Join(peer.Roles, ","),
			peer.QueueDepth,
			peer.MaxQueue,
			peer.ActiveRuns,
		))
		metaPeers = append(metaPeers, map[string]any{
			"id":             peer.ID,
			"enabled":        peer.Enabled,
			"base_url":       peer.BaseURL,
			"capabilities":   peer.Capabilities,
			"roles":          peer.Roles,
			"priority":       peer.Priority,
			"max_concurrent": peer.MaxConcurrent,
			"max_queue":      peer.MaxQueue,
			"available":      peer.Available,
			"queue_depth":    peer.QueueDepth,
			"active_runs":    peer.ActiveRuns,
			"last_error":     peer.LastError,
			"updated_at":     peer.UpdatedAt,
		})
	}
	text := "No peers configured."
	if len(lines) > 0 {
		text = "Peers:\n- " + strings.Join(lines, "\n- ")
	}
	return ToolResult{
		Text:     text,
		Metadata: map[string]any{"peers": metaPeers},
	}, nil
}

