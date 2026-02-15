package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/federation"
	"github.com/grixate/squidbot/internal/subagent"
	"github.com/grixate/squidbot/internal/tools"
)

const federationIdempotencyTTL = 24 * time.Hour

func (e *Engine) federationNodeID(cfg config.Config) string {
	if nodeID := strings.TrimSpace(cfg.Runtime.Federation.NodeID); nodeID != "" {
		return nodeID
	}
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		return "squidbot-node"
	}
	return "squidbot-" + strings.ToLower(strings.TrimSpace(host))
}

func toFederationContext(packet subagent.ContextPacket) federation.ContextPacket {
	return federation.ContextPacket{
		Mode:           string(packet.Mode),
		SystemPrompt:   packet.SystemPrompt,
		History:        packet.History,
		MemorySnippets: append([]string(nil), packet.MemorySnippets...),
		Attachments:    append([]string(nil), packet.Attachments...),
		CreatedAt:      packet.CreatedAt,
		Checksum:       packet.Checksum,
	}
}

func toSubagentContext(packet federation.ContextPacket) subagent.ContextPacket {
	return subagent.ContextPacket{
		Mode:           subagent.NormalizeContextMode(packet.Mode),
		SystemPrompt:   packet.SystemPrompt,
		History:        packet.History,
		MemorySnippets: append([]string(nil), packet.MemorySnippets...),
		Attachments:    append([]string(nil), packet.Attachments...),
		CreatedAt:      packet.CreatedAt,
		Checksum:       packet.Checksum,
	}
}

func mapDelegationStatus(status federation.DelegationStatus) subagent.Status {
	switch status {
	case federation.StatusQueued:
		return subagent.StatusQueued
	case federation.StatusRunning:
		return subagent.StatusRunning
	case federation.StatusSucceeded:
		return subagent.StatusSucceeded
	case federation.StatusTimedOut:
		return subagent.StatusTimedOut
	case federation.StatusCancelled:
		return subagent.StatusCancelled
	default:
		return subagent.StatusFailed
	}
}

func (e *Engine) rankedFederationPeers(cfg config.Config, req tools.SpawnRequest) ([]config.FederationPeerConfig, federation.RouteDecision) {
	peers := make([]config.FederationPeerConfig, 0, len(cfg.Runtime.Federation.Peers))
	requiredCaps := federation.NormalizeCapabilityList(req.RequiredCapabilities)
	preferredRoles := federation.NormalizeCapabilityList(req.PreferredRoles)
	preferredPeer := strings.TrimSpace(req.PreferredPeerID)
	for _, peer := range cfg.Runtime.Federation.Peers {
		if !peer.Enabled || strings.TrimSpace(peer.ID) == "" || strings.TrimSpace(peer.BaseURL) == "" {
			continue
		}
		caps := federation.NormalizeCapabilityList(peer.Capabilities)
		missing := false
		for _, required := range requiredCaps {
			found := false
			for _, current := range caps {
				if current == required {
					found = true
					break
				}
			}
			if !found {
				missing = true
				break
			}
		}
		if missing {
			continue
		}
		peers = append(peers, peer)
	}
	sort.SliceStable(peers, func(i, j int) bool {
		return peers[i].Priority < peers[j].Priority
	})
	if preferredPeer != "" {
		sort.SliceStable(peers, func(i, j int) bool {
			left := strings.EqualFold(peers[i].ID, preferredPeer)
			right := strings.EqualFold(peers[j].ID, preferredPeer)
			if left == right {
				return false
			}
			return left
		})
	}
	if len(preferredRoles) > 0 {
		roleRank := func(peer config.FederationPeerConfig) int {
			roles := federation.NormalizeCapabilityList(peer.Roles)
			for _, preferred := range preferredRoles {
				for _, role := range roles {
					if role == preferred {
						return 0
					}
				}
			}
			return 1
		}
		sort.SliceStable(peers, func(i, j int) bool {
			return roleRank(peers[i]) < roleRank(peers[j])
		})
	}
	candidateIDs := make([]string, 0, len(peers))
	for _, peer := range peers {
		candidateIDs = append(candidateIDs, peer.ID)
	}
	decision := federation.RouteDecision{
		CandidatePeerIDs: candidateIDs,
		Reason:           "sorted by priority/capabilities/roles",
	}
	if len(peers) > 0 {
		decision.SelectedPeerID = peers[0].ID
	}
	return peers, decision
}

func (e *Engine) isRetryableFederationError(err error) bool {
	if err == nil {
		return false
	}
	var reqErr *federation.RequestError
	if errors.As(err, &reqErr) {
		return reqErr.Retryable()
	}
	return true
}

func (e *Engine) spawnRemoteSubtask(ctx context.Context, req tools.SpawnRequest) (tools.SpawnResponse, error) {
	cfg := e.currentConfig()
	if !cfg.Runtime.Federation.Enabled {
		return tools.SpawnResponse{}, fmt.Errorf("federation is disabled")
	}
	packet, err := e.buildSubagentContextPacket(ctx, req)
	if err != nil {
		return tools.SpawnResponse{}, err
	}
	peers, route := e.rankedFederationPeers(cfg, req)
	if len(peers) == 0 {
		return tools.SpawnResponse{}, fmt.Errorf("no federation peers matched this task")
	}
	allowFallback := cfg.Runtime.Federation.AutoFallback
	if req.AllowFallback != nil {
		allowFallback = *req.AllowFallback
	}
	request := federation.DelegationRequest{
		ID:                   e.nextID(),
		Task:                 req.Task,
		Label:                req.Label,
		SessionID:            req.SessionID,
		Channel:              req.Channel,
		ChatID:               req.ChatID,
		SenderID:             req.SenderID,
		TimeoutSec:           req.TimeoutSec,
		MaxAttempts:          req.MaxAttempts,
		Depth:                req.Depth + 1,
		Context:              toFederationContext(packet),
		Metadata:             map[string]any{"source": "spawn_remote"},
		RequiredCapabilities: federation.NormalizeCapabilityList(req.RequiredCapabilities),
		PreferredRoles:       federation.NormalizeCapabilityList(req.PreferredRoles),
	}
	originNodeID := e.federationNodeID(cfg)
	idempotencyKey := e.nextID()
	maxRetries := cfg.Runtime.Federation.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	retryBackoff := time.Duration(cfg.Runtime.Federation.RetryBackoffMs) * time.Millisecond
	if retryBackoff < 0 {
		retryBackoff = 0
	}
	attempts := make([]federation.DeliveryAttempt, 0, len(peers)*(maxRetries+1))
	var selected config.FederationPeerConfig
	var submitted federation.DelegationRun
	var lastErr error
	for index, peer := range peers {
		for attempt := 1; attempt <= maxRetries+1; attempt++ {
			started := time.Now().UTC()
			run, callErr := e.federationClient.Submit(ctx, peer, request, originNodeID, idempotencyKey)
			finished := time.Now().UTC()
			delivery := federation.DeliveryAttempt{
				PeerID:      peer.ID,
				Attempt:     attempt,
				StartedAt:   started,
				FinishedAt:  finished,
				DurationMS:  finished.Sub(started).Milliseconds(),
				Idempotency: idempotencyKey,
			}
			if callErr == nil {
				selected = peer
				submitted = run
				attempts = append(attempts, delivery)
				break
			}
			lastErr = callErr
			delivery.Error = callErr.Error()
			delivery.Retryable = e.isRetryableFederationError(callErr)
			var reqErr *federation.RequestError
			if errors.As(callErr, &reqErr) {
				delivery.StatusCode = reqErr.StatusCode
			}
			attempts = append(attempts, delivery)
			if !delivery.Retryable || attempt >= maxRetries+1 {
				break
			}
			if retryBackoff > 0 {
				select {
				case <-ctx.Done():
					return tools.SpawnResponse{}, ctx.Err()
				case <-time.After(retryBackoff):
				}
			}
		}
		if strings.TrimSpace(selected.ID) != "" {
			break
		}
		if !allowFallback || index >= len(peers)-1 {
			break
		}
		e.metrics.FallbackCount.Add(1)
	}
	if strings.TrimSpace(selected.ID) == "" {
		if lastErr != nil {
			return tools.SpawnResponse{}, fmt.Errorf("remote delegation failed: %w", lastErr)
		}
		return tools.SpawnResponse{}, fmt.Errorf("remote delegation failed: no peer accepted request")
	}
	submitted.PeerID = selected.ID
	submitted.Route = route
	submitted.DeliveryAttempt = attempts

	if req.Wait {
		waitTimeout := time.Duration(max(submitted.TimeoutSec, 1)+30) * time.Second
		if req.TimeoutSec > 0 {
			waitTimeout = time.Duration(req.TimeoutSec+30) * time.Second
		}
		waited, waitErr := e.waitRemoteDelegation(ctx, selected, submitted.ID, originNodeID, waitTimeout)
		status := mapDelegationStatus(waited.Status)
		text := fmt.Sprintf("Remote subagent [%s] finished with status %s (run_id: %s, peer: %s).", req.Label, waited.Status, waited.ID, selected.ID)
		if waited.Result != nil && strings.TrimSpace(waited.Result.Summary) != "" {
			text += "\n" + waited.Result.Summary
		}
		if waitErr != nil {
			text += "\nWait interrupted: " + waitErr.Error()
		}
		var result *subagent.Result
		if waited.Result != nil {
			result = &subagent.Result{
				Summary:       waited.Result.Summary,
				Output:        waited.Result.Output,
				ArtifactPaths: append([]string(nil), waited.Result.ArtifactPaths...),
			}
			e.recordFederationUsage(ctx, waited, selected.ID)
		}
		return tools.SpawnResponse{
			RunID:  waited.ID,
			Status: status,
			Result: result,
			Text:   text,
		}, nil
	}
	go e.watchRemoteDelegation(submitted, selected, originNodeID)
	return tools.SpawnResponse{
		RunID:  submitted.ID,
		Status: mapDelegationStatus(submitted.Status),
		Text:   fmt.Sprintf("Remote subagent [%s] started on peer %s (run_id: %s). I will notify on completion.", req.Label, selected.ID, submitted.ID),
	}, nil
}

func (e *Engine) waitRemoteDelegation(ctx context.Context, peer config.FederationPeerConfig, runID, originNodeID string, timeout time.Duration) (federation.DelegationRun, error) {
	waitCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	ticker := time.NewTicker(700 * time.Millisecond)
	defer ticker.Stop()
	last := federation.DelegationRun{}
	for {
		run, err := e.federationClient.Status(waitCtx, peer, runID, originNodeID)
		if err != nil {
			return last, err
		}
		last = run
		if run.Status.Terminal() {
			if run.Result == nil {
				if resultRun, resultErr := e.federationClient.Result(waitCtx, peer, runID, originNodeID); resultErr == nil {
					last = resultRun
				}
			}
			return last, nil
		}
		select {
		case <-waitCtx.Done():
			return last, waitCtx.Err()
		case <-ticker.C:
		}
	}
}

func (e *Engine) watchRemoteDelegation(run federation.DelegationRun, peer config.FederationPeerConfig, originNodeID string) {
	waited, err := e.waitRemoteDelegation(context.Background(), peer, run.ID, originNodeID, time.Duration(max(run.TimeoutSec, 1)+60)*time.Second)
	if err != nil {
		e.log.Printf("remote delegation watcher failed run=%s peer=%s err=%v", run.ID, peer.ID, err)
		return
	}
	lines := []string{
		"[Federation completed]",
		"",
		fmt.Sprintf("Run: %s", waited.ID),
		fmt.Sprintf("Peer: %s", peer.ID),
		fmt.Sprintf("Status: %s", waited.Status),
		fmt.Sprintf("Task: %s", waited.Task),
	}
	if waited.Result != nil && strings.TrimSpace(waited.Result.Summary) != "" {
		lines = append(lines, "", "Summary:", waited.Result.Summary)
	}
	if strings.TrimSpace(waited.Error) != "" {
		lines = append(lines, "", "Error:", waited.Error)
	}
	e.send(waited.Channel, waited.ChatID, strings.Join(lines, "\n"), map[string]interface{}{
		"session_id": waited.SessionID,
		"source":     "federation",
		"run_id":     waited.ID,
		"status":     waited.Status,
		"peer_id":    peer.ID,
	})
	e.recordFederationUsage(context.Background(), waited, peer.ID)
}

func (e *Engine) recordFederationUsage(ctx context.Context, run federation.DelegationRun, peerID string) {
	if e == nil || e.store == nil {
		return
	}
	outputLen := 0
	if run.Result != nil {
		outputLen = len(strings.TrimSpace(run.Result.Output))
		if outputLen == 0 {
			outputLen = len(strings.TrimSpace(run.Result.Summary))
		}
	}
	if outputLen <= 0 {
		outputLen = 1
	}
	charsPerToken := max(e.currentConfig().Runtime.TokenSafety.EstimateCharsPerToken, 1)
	estimatedTotal := uint64(max(outputLen/charsPerToken, 1))
	_ = e.store.AddBudgetUsage(ctx, "federation:"+strings.TrimSpace(run.ID), 0, estimatedTotal, estimatedTotal)
	if strings.TrimSpace(peerID) != "" {
		_ = e.store.AddBudgetUsage(ctx, "peer:"+strings.TrimSpace(peerID), 0, estimatedTotal, estimatedTotal)
	}
}

func (e *Engine) FederationSubmit(ctx context.Context, req federation.DelegationRequest, originNodeID, idempotencyKey string) (federation.DelegationRun, error) {
	if e == nil || e.store == nil {
		return federation.DelegationRun{}, fmt.Errorf("engine is not configured")
	}
	req.Task = strings.TrimSpace(req.Task)
	if req.Task == "" {
		return federation.DelegationRun{}, fmt.Errorf("task is required")
	}
	cfg := e.currentConfig()
	originNodeID = strings.TrimSpace(originNodeID)
	if originNodeID == "" {
		originNodeID = "unknown"
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey != "" {
		record, err := e.store.GetFederationIdempotency(ctx, originNodeID, idempotencyKey)
		if err == nil {
			e.metrics.IdempotencyHits.Add(1)
			return e.store.GetFederationRun(ctx, record.RunID)
		}
	}
	runID := strings.TrimSpace(req.ID)
	if runID == "" {
		runID = e.nextID()
	}
	timeoutSec := req.TimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = cfg.Runtime.Subagents.DefaultTimeoutSec
	}
	maxAttempts := req.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = max(cfg.Runtime.Subagents.MaxAttempts, 1)
	}
	createdAt := time.Now().UTC()
	run := federation.DelegationRun{
		ID:             runID,
		OriginNodeID:   originNodeID,
		IdempotencyKey: idempotencyKey,
		SessionID:      strings.TrimSpace(req.SessionID),
		Channel:        strings.TrimSpace(req.Channel),
		ChatID:         strings.TrimSpace(req.ChatID),
		SenderID:       strings.TrimSpace(req.SenderID),
		Task:           req.Task,
		Label:          strings.TrimSpace(req.Label),
		Status:         federation.StatusQueued,
		CreatedAt:      createdAt,
		TimeoutSec:     timeoutSec,
		MaxAttempts:    maxAttempts,
		Depth:          req.Depth,
		Context:        req.Context,
		Metadata:       req.Metadata,
		RequiredCaps:   federation.NormalizeCapabilityList(req.RequiredCapabilities),
		PreferredRoles: federation.NormalizeCapabilityList(req.PreferredRoles),
	}
	if err := e.store.PutFederationRun(ctx, run); err != nil {
		return federation.DelegationRun{}, err
	}
	_ = e.store.AppendFederationEvent(ctx, federation.Event{
		RunID:     run.ID,
		Status:    federation.StatusQueued,
		Message:   "delegation accepted",
		CreatedAt: createdAt,
	})
	if idempotencyKey != "" {
		_ = e.store.PutFederationIdempotency(ctx, federation.IdempotencyRecord{
			OriginNodeID:   originNodeID,
			IdempotencyKey: idempotencyKey,
			RunID:          run.ID,
			CreatedAt:      createdAt,
			ExpiresAt:      createdAt.Add(federationIdempotencyTTL),
		})
	}
	e.metrics.DelegationsSubmitted.Add(1)
	go e.executeFederationRun(run.ID)
	return run, nil
}

func (e *Engine) executeFederationRun(runID string) {
	ctx := context.Background()
	run, err := e.store.GetFederationRun(ctx, runID)
	if err != nil || run.Status.Terminal() {
		return
	}
	startedAt := time.Now().UTC()
	run.Status = federation.StatusRunning
	run.Attempt++
	run.StartedAt = &startedAt
	_ = e.store.PutFederationRun(ctx, run)
	_ = e.store.AppendFederationEvent(ctx, federation.Event{
		RunID:     run.ID,
		Status:    run.Status,
		Message:   "delegation started",
		CreatedAt: startedAt,
	})
	timeout := time.Duration(max(run.TimeoutSec, 1)) * time.Second
	runCtx, cancel := context.WithTimeout(context.Background(), timeout)
	e.fedCancelMu.Lock()
	e.fedCancels[run.ID] = cancel
	e.fedCancelMu.Unlock()
	defer func() {
		cancel()
		e.fedCancelMu.Lock()
		delete(e.fedCancels, run.ID)
		e.fedCancelMu.Unlock()
	}()
	artifactDir := filepath.Join(config.WorkspacePath(e.currentConfig()), ".squidbot", "federation", run.ID)
	_ = os.MkdirAll(artifactDir, 0o755)
	result, runErr := e.runSubtask(runCtx, subagent.Run{
		ID:          "federated-" + run.ID,
		SessionID:   run.SessionID,
		Channel:     run.Channel,
		ChatID:      run.ChatID,
		SenderID:    run.SenderID,
		Label:       run.Label,
		Task:        run.Task,
		CreatedAt:   run.CreatedAt,
		TimeoutSec:  run.TimeoutSec,
		MaxAttempts: 1,
		Depth:       run.Depth,
		ArtifactDir: artifactDir,
		Context:     toSubagentContext(run.Context),
	})
	finishedAt := time.Now().UTC()
	if latest, getErr := e.store.GetFederationRun(ctx, run.ID); getErr == nil && latest.Status == federation.StatusCancelled {
		if latest.FinishedAt == nil {
			latest.FinishedAt = &finishedAt
			_ = e.store.PutFederationRun(ctx, latest)
		}
		return
	}
	run.FinishedAt = &finishedAt
	if runErr == nil {
		run.Status = federation.StatusSucceeded
		run.Result = &federation.DelegationResult{
			Summary:       result.Summary,
			Output:        result.Output,
			ArtifactPaths: append([]string(nil), result.ArtifactPaths...),
		}
		run.Error = ""
		e.metrics.DelegationsSucceeded.Add(1)
	} else {
		if errors.Is(runErr, context.DeadlineExceeded) {
			run.Status = federation.StatusTimedOut
		} else if errors.Is(runErr, context.Canceled) {
			run.Status = federation.StatusCancelled
		} else {
			run.Status = federation.StatusFailed
		}
		run.Error = runErr.Error()
		e.metrics.DelegationsFailed.Add(1)
	}
	e.metrics.DelegationLatencyMS.Store(uint64(max(int(finishedAt.Sub(startedAt).Milliseconds()), 0)))
	_ = e.store.PutFederationRun(ctx, run)
	_ = e.store.AppendFederationEvent(ctx, federation.Event{
		RunID:     run.ID,
		Status:    run.Status,
		Message:   strings.TrimSpace(run.Error),
		CreatedAt: finishedAt,
	})
}

func (e *Engine) FederationStatus(ctx context.Context, runID string) (federation.DelegationRun, error) {
	return e.store.GetFederationRun(ctx, runID)
}

func (e *Engine) FederationResult(ctx context.Context, runID string) (federation.DelegationRun, error) {
	run, err := e.store.GetFederationRun(ctx, runID)
	if err != nil {
		return federation.DelegationRun{}, err
	}
	if !run.Status.Terminal() {
		return run, fmt.Errorf("run %s is not complete", run.ID)
	}
	return run, nil
}

func (e *Engine) FederationCancel(ctx context.Context, runID string) (federation.DelegationRun, error) {
	run, err := e.store.GetFederationRun(ctx, runID)
	if err != nil {
		return federation.DelegationRun{}, err
	}
	if run.Status.Terminal() {
		return run, nil
	}
	now := time.Now().UTC()
	run.Status = federation.StatusCancelled
	run.Error = "cancelled"
	run.FinishedAt = &now
	if err := e.store.PutFederationRun(ctx, run); err != nil {
		return federation.DelegationRun{}, err
	}
	_ = e.store.AppendFederationEvent(ctx, federation.Event{
		RunID:     run.ID,
		Status:    run.Status,
		Message:   "cancelled",
		CreatedAt: now,
	})
	e.fedCancelMu.Lock()
	cancel := e.fedCancels[run.ID]
	e.fedCancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
	return run, nil
}

func (e *Engine) FederationHealth(ctx context.Context) (federation.PeerHealth, error) {
	_ = ctx
	cfg := e.currentConfig()
	health := federation.PeerHealth{
		PeerID:     e.federationNodeID(cfg),
		Available:  true,
		QueueDepth: int(e.metrics.SubagentQueueDepth.Load()),
		MaxQueue:   cfg.Runtime.Subagents.MaxQueue,
		ActiveRuns: int(e.metrics.SubagentRunning.Load()),
		UpdatedAt:  time.Now().UTC(),
	}
	return health, nil
}

func (e *Engine) FederationRuns(ctx context.Context, sessionID string, status federation.DelegationStatus, limit int) ([]federation.DelegationRun, error) {
	return e.store.ListFederationRuns(ctx, sessionID, status, limit)
}

func (e *Engine) federationPeersStatus(ctx context.Context) ([]tools.FederationPeerInfo, error) {
	cfg := e.currentConfig()
	healthByPeer := map[string]federation.PeerHealth{}
	if records, err := e.store.ListFederationPeerHealth(ctx, 0); err == nil {
		for _, item := range records {
			healthByPeer[item.PeerID] = item
		}
	}
	peers := make([]tools.FederationPeerInfo, 0, len(cfg.Runtime.Federation.Peers))
	for _, peer := range cfg.Runtime.Federation.Peers {
		info := tools.FederationPeerInfo{
			ID:            strings.TrimSpace(peer.ID),
			Enabled:       peer.Enabled,
			BaseURL:       strings.TrimSpace(peer.BaseURL),
			Capabilities:  federation.NormalizeCapabilityList(peer.Capabilities),
			Roles:         federation.NormalizeCapabilityList(peer.Roles),
			Priority:      peer.Priority,
			MaxConcurrent: peer.MaxConcurrent,
			MaxQueue:      peer.MaxQueue,
		}
		if health, ok := healthByPeer[info.ID]; ok {
			info.Available = health.Available
			info.QueueDepth = health.QueueDepth
			info.ActiveRuns = health.ActiveRuns
			info.LastError = health.Error
			info.UpdatedAt = health.UpdatedAt
		}
		peers = append(peers, info)
	}
	sort.Slice(peers, func(i, j int) bool {
		if peers[i].Priority == peers[j].Priority {
			return peers[i].ID < peers[j].ID
		}
		return peers[i].Priority < peers[j].Priority
	})
	return peers, nil
}
