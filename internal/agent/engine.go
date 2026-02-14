package agent

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	mrand "math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/grixate/squidbot/internal/budget"
	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/memory"
	"github.com/grixate/squidbot/internal/mission"
	"github.com/grixate/squidbot/internal/provider"
	"github.com/grixate/squidbot/internal/runtime/actor"
	"github.com/grixate/squidbot/internal/subagent"
	"github.com/grixate/squidbot/internal/telemetry"
	"github.com/grixate/squidbot/internal/tools"
)

type Store interface {
	ConversationStore
	KVStore
	SchedulerStore
	ToolEventStore
	MissionStore
	SubagentStore
	BudgetStore
	SaveCheckpoint(ctx context.Context, sessionID string, payload []byte) error
	LoadCheckpoint(ctx context.Context, sessionID string) ([]byte, error)
}

type Engine struct {
	cfg                 config.Config
	provider            provider.LLMProvider
	model               string
	store               Store
	metrics             *telemetry.Metrics
	log                 *log.Logger
	actors              *actor.System
	outbound            chan OutboundMessage
	policy              *tools.PathPolicy
	memory              *memory.Manager
	subagents           *subagent.Manager
	budgetGuard         *budget.Guard
	ulidMu              sync.Mutex
	stateMu             sync.RWMutex
	tokenSafetyMu       sync.Mutex
	tokenSafetyCached   budget.Settings
	tokenSafetyCachedAt time.Time
	tokenSafetyCacheTTL time.Duration
	entropy             *ulid.MonotonicEntropy
}

type processRequest struct {
	Msg InboundMessage
}

func NewEngine(cfg config.Config, providerClient provider.LLMProvider, model string, store Store, metrics *telemetry.Metrics, logger *log.Logger) (*Engine, error) {
	policy, err := tools.NewPathPolicy(config.WorkspacePath(cfg))
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = log.Default()
	}
	if metrics == nil {
		metrics = &telemetry.Metrics{}
	}
	engine := &Engine{
		cfg:                 cfg,
		provider:            providerClient,
		model:               model,
		store:               store,
		metrics:             metrics,
		log:                 logger,
		outbound:            make(chan OutboundMessage, 512),
		policy:              policy,
		memory:              memory.NewManager(cfg),
		budgetGuard:         budget.NewGuard(store, metrics),
		tokenSafetyCacheTTL: 2 * time.Second,
		entropy:             ulid.Monotonic(mrand.New(mrand.NewSource(time.Now().UnixNano())), 0),
	}
	system := actor.NewSystem(engine.newSessionHandler, cfg.Runtime.MailboxSize, cfg.Runtime.ActorIdleTTL.Duration)
	system.SetActorHooks(func() { engine.metrics.ActiveActors.Add(1) }, func() { engine.metrics.ActiveActors.Add(-1) })
	engine.actors = system
	subCfg := cfg.Runtime.Subagents
	engine.subagents = subagent.NewManager(subagent.Options{
		Enabled:          subCfg.Enabled,
		MaxConcurrent:    subCfg.MaxConcurrent,
		MaxQueue:         subCfg.MaxQueue,
		DefaultTimeout:   time.Duration(subCfg.DefaultTimeoutSec) * time.Second,
		MaxAttempts:      subCfg.MaxAttempts,
		RetryBackoff:     time.Duration(subCfg.RetryBackoffSec) * time.Second,
		MaxDepth:         subCfg.MaxDepth,
		NotifyOnComplete: subCfg.NotifyOnComplete,
		NextID:           engine.nextID,
	}, store, engine.runSubtask, engine.notifySubagentCompletion, metrics)
	if err := engine.subagents.Start(context.Background()); err != nil {
		return nil, err
	}
	return engine, nil
}

func (e *Engine) Outbound() <-chan OutboundMessage {
	return e.outbound
}

func (e *Engine) EmitOutbound(channel, chatID, content string, metadata map[string]interface{}) {
	e.send(channel, chatID, content, metadata)
}

func (e *Engine) Close() error {
	if e.subagents != nil {
		e.subagents.Stop()
	}
	return e.actors.Stop()
}

func (e *Engine) Submit(ctx context.Context, msg InboundMessage) (Ack, error) {
	if msg.RequestID == "" {
		msg.RequestID = e.nextID()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	if strings.TrimSpace(msg.SessionID) == "" {
		msg.SessionID = msg.Channel + ":" + msg.ChatID
	}
	_, err := e.actors.Submit(ctx, msg.SessionID, processRequest{Msg: msg}, false)
	if err != nil {
		return Ack{}, err
	}
	e.metrics.InboundCount.Add(1)
	return Ack{RequestID: msg.RequestID}, nil
}

func (e *Engine) Ask(ctx context.Context, msg InboundMessage) (string, error) {
	if msg.RequestID == "" {
		msg.RequestID = e.nextID()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	if strings.TrimSpace(msg.SessionID) == "" {
		msg.SessionID = msg.Channel + ":" + msg.ChatID
	}
	res, err := e.actors.Submit(ctx, msg.SessionID, processRequest{Msg: msg}, true)
	if err != nil {
		return "", err
	}
	response, _ := res.(string)
	return response, nil
}

func (e *Engine) Snapshot(ctx context.Context, sessionID string) (SessionSnapshot, error) {
	history, err := e.store.Window(ctx, sessionID, 100)
	if err != nil {
		return SessionSnapshot{}, err
	}
	return SessionSnapshot{SessionID: sessionID, Messages: history}, nil
}

func (e *Engine) send(channel, chatID, content string, metadata map[string]interface{}) {
	msg := OutboundMessage{Channel: channel, ChatID: chatID, Content: content, Metadata: make(map[string]any)}
	for k, v := range metadata {
		msg.Metadata[k] = v
	}
	select {
	case e.outbound <- msg:
		e.metrics.OutboundCount.Add(1)
	default:
		e.log.Printf("outbound channel full; dropping message channel=%s chat_id=%s", channel, chatID)
	}
}

func (e *Engine) nextID() string {
	e.ulidMu.Lock()
	defer e.ulidMu.Unlock()
	return ulid.MustNew(ulid.Timestamp(time.Now()), e.entropy).String()
}

func (e *Engine) currentConfig() config.Config {
	e.stateMu.RLock()
	defer e.stateMu.RUnlock()
	return e.cfg
}

func (e *Engine) currentProviderModel() (provider.LLMProvider, string) {
	e.stateMu.RLock()
	defer e.stateMu.RUnlock()
	return e.provider, e.model
}

func (e *Engine) newSessionHandler(sessionID string) (actor.SessionHandler, error) {
	h := &sessionHandler{engine: e, sessionID: sessionID}
	if checkpoint, err := e.store.LoadCheckpoint(context.Background(), sessionID); err == nil && len(checkpoint) > 0 {
		var restored struct {
			LastRequestID string `json:"last_request_id"`
		}
		if json.Unmarshal(checkpoint, &restored) == nil {
			h.lastRequestID = restored.LastRequestID
		}
	}
	return h, nil
}

type sessionHandler struct {
	engine        *Engine
	sessionID     string
	lastRequestID string
}

func (h *sessionHandler) Handle(ctx context.Context, payload interface{}) (interface{}, error) {
	req, ok := payload.(processRequest)
	if !ok {
		return nil, fmt.Errorf("invalid payload type %T", payload)
	}
	response, err := h.process(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	h.lastRequestID = req.Msg.RequestID
	checkpoint, _ := json.Marshal(map[string]interface{}{"last_request_id": h.lastRequestID, "updated_at": time.Now().UTC()})
	_ = h.engine.store.SaveCheckpoint(context.Background(), h.sessionID, checkpoint)
	return response, nil
}

func (h *sessionHandler) Close() error { return nil }

func (h *sessionHandler) process(ctx context.Context, msg InboundMessage) (string, error) {
	h.engine.metrics.ActiveTurns.Add(1)
	defer h.engine.metrics.ActiveTurns.Add(-1)

	cfg := h.engine.currentConfig()
	turnTimeout := time.Duration(cfg.Agents.Defaults.TurnTimeoutSec) * time.Second
	if turnTimeout <= 0 {
		turnTimeout = 120 * time.Second
	}
	turnCtx, cancel := context.WithTimeout(ctx, turnTimeout)
	defer cancel()

	history, err := h.engine.store.Window(turnCtx, h.sessionID, 50)
	if err != nil {
		return "", err
	}
	systemPrompt := buildSystemPrompt(cfg, msg.Content)
	messages := buildMessages(systemPrompt, history, msg.Content)
	registry, err := h.engine.buildRegistry(msg)
	if err != nil {
		return "", err
	}

	maxHops := cfg.Agents.Defaults.MaxToolIterations
	if maxHops <= 0 {
		maxHops = 20
	}
	finalContent := ""
	budgetWarnings := []string{}

	for i := 0; i < maxHops; i++ {
		settings := h.engine.effectiveTokenSafety(turnCtx)
		scopeLimits := []budget.ScopeLimit{
			{Key: "global", HardLimitTokens: settings.GlobalHardLimitTokens, SoftThresholdPct: settings.GlobalSoftThresholdPct},
		}
		if strings.TrimSpace(h.sessionID) != "" {
			scopeLimits = append(scopeLimits, budget.ScopeLimit{
				Key:              "session:" + strings.TrimSpace(h.sessionID),
				HardLimitTokens:  settings.SessionHardLimitTokens,
				SoftThresholdPct: settings.SessionSoftThresholdPct,
			})
		}
		plannedTokens := uint64(max(cfg.Agents.Defaults.MaxTokens, 1))
		preflight, preflightErr := h.engine.budgetGuard.Preflight(turnCtx, settings, scopeLimits, plannedTokens)
		if preflightErr != nil {
			var limitErr *budget.LimitError
			if errors.As(preflightErr, &limitErr) {
				finalContent = h.engine.formatBudgetLimitMessage(limitErr)
				break
			}
			return "", preflightErr
		}
		h.engine.metrics.ProviderCalls.Add(1)
		providerClient, model := h.engine.currentProviderModel()
		response, chatErr := providerClient.Chat(turnCtx, provider.ChatRequest{
			Messages:    messages,
			Tools:       registry.Definitions(),
			Model:       model,
			MaxTokens:   cfg.Agents.Defaults.MaxTokens,
			Temperature: cfg.Agents.Defaults.Temperature,
		})
		if chatErr != nil {
			h.engine.budgetGuard.Abort(turnCtx, preflight)
			h.engine.metrics.ProviderErrors.Add(1)
			return "", chatErr
		}
		commit, commitErr := h.engine.budgetGuard.Commit(turnCtx, settings, scopeLimits, preflight, budget.Usage{
			PromptTokens:     response.Usage.PromptTokens,
			CompletionTokens: response.Usage.CompletionTokens,
			TotalTokens:      response.Usage.TotalTokens,
			OutputChars:      len(response.Content),
		})
		if commitErr != nil {
			h.engine.log.Printf("failed to commit token budget usage: %v", commitErr)
		}
		h.engine.recordUsageDay(turnCtx,
			uint64(max(response.Usage.PromptTokens, 0)),
			uint64(max(response.Usage.CompletionTokens, 0)),
			commit.TotalTokens,
		)
		for _, warning := range commit.Warnings {
			budgetWarnings = append(budgetWarnings,
				fmt.Sprintf("%s at %d%% of hard limit", warning.Scope, warning.ThresholdPct))
		}

		if response.HasToolCalls() {
			messages = append(messages, provider.Message{Role: "assistant", Content: response.Content, ToolCalls: response.ToolCalls})
			for _, tc := range response.ToolCalls {
				h.engine.metrics.ToolCalls.Add(1)
				toolTimeout := time.Duration(cfg.Agents.Defaults.ToolTimeoutSec) * time.Second
				if toolTimeout <= 0 {
					toolTimeout = 60 * time.Second
				}
				toolCtx, toolCancel := context.WithTimeout(turnCtx, toolTimeout)
				result, toolErr := registry.Execute(toolCtx, tc.Name, tc.Arguments)
				toolCancel()
				if toolErr != nil {
					h.engine.metrics.ToolErrors.Add(1)
					result = tools.ToolResult{Text: toolErr.Error()}
				}

				_ = h.engine.store.AppendToolEvent(turnCtx, ToolEvent{
					SessionID: h.sessionID,
					ToolName:  tc.Name,
					Input:     string(tc.Arguments),
					Output:    result.Text,
				})
				messages = append(messages, provider.Message{Role: "tool", ToolCallID: tc.ID, Name: tc.Name, Content: result.Text})
			}
			continue
		}

		finalContent = strings.TrimSpace(response.Content)
		if finalContent == "" {
			finalContent = "I've completed processing but have no response to provide."
		}
		break
	}

	if strings.TrimSpace(finalContent) == "" {
		finalContent = "I've completed processing but have no response to provide."
	}
	if len(budgetWarnings) > 0 {
		finalContent = strings.TrimSpace(finalContent) + "\n\n[Token safety]\n- " + strings.Join(budgetWarnings, "\n- ")
	}

	if err := h.engine.store.AppendTurn(turnCtx, Turn{SessionID: h.sessionID, Role: "user", Content: msg.Content}); err != nil {
		h.engine.log.Printf("failed to persist user turn: %v", err)
	}
	if err := h.engine.store.AppendTurn(turnCtx, Turn{SessionID: h.sessionID, Role: "assistant", Content: finalContent}); err != nil {
		h.engine.log.Printf("failed to persist assistant turn: %v", err)
	}
	_ = h.engine.store.SaveSessionMeta(turnCtx, h.sessionID, map[string]interface{}{"last_channel": msg.Channel, "last_chat_id": msg.ChatID})

	if msg.Channel != "cli" {
		h.engine.send(msg.Channel, msg.ChatID, finalContent, map[string]interface{}{"session_id": msg.SessionID})
	}
	h.engine.appendDailyMemory(turnCtx, msg, finalContent)
	return finalContent, nil
}

func (e *Engine) appendDailyMemory(ctx context.Context, msg InboundMessage, response string) {
	if e.memory == nil || !e.memory.Enabled() {
		return
	}
	intent := msg.Content
	if len(intent) > 240 {
		intent = intent[:237] + "..."
	}
	outcome := response
	if len(outcome) > 320 {
		outcome = outcome[:317] + "..."
	}
	followUp := suggestsFollowUp(response)
	if err := e.memory.AppendDailyLog(ctx, memory.DailyEntry{
		Time:      time.Now().UTC(),
		Source:    "conversation",
		SessionID: msg.SessionID,
		Intent:    intent,
		Outcome:   outcome,
		FollowUp:  followUp,
	}); err != nil {
		e.log.Printf("failed to append daily memory: %v", err)
	}
}

func (e *Engine) RecordHeartbeat(ctx context.Context, prompt, response string) {
	if e.memory == nil || !e.memory.Enabled() {
		return
	}
	intent := prompt
	if len(intent) > 240 {
		intent = intent[:237] + "..."
	}
	outcome := response
	if len(outcome) > 320 {
		outcome = outcome[:317] + "..."
	}
	if err := e.memory.AppendDailyLog(ctx, memory.DailyEntry{
		Time:      time.Now().UTC(),
		Source:    "heartbeat",
		SessionID: "system:heartbeat",
		Intent:    intent,
		Outcome:   outcome,
		FollowUp:  suggestsFollowUp(response),
	}); err != nil {
		e.log.Printf("failed to append heartbeat memory: %v", err)
	}
}

func suggestsFollowUp(content string) bool {
	lower := strings.ToLower(content)
	markers := []string{
		"follow-up",
		"follow up",
		"waiting on",
		"blocked",
		"next step",
		"need input",
		"action required",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func (e *Engine) buildRegistry(msg InboundMessage) (*tools.Registry, error) {
	cfg := e.currentConfig()
	registry := tools.NewRegistry()
	registry.Register(tools.NewReadFileTool(e.policy))
	registry.Register(tools.NewWriteFileTool(e.policy))
	registry.Register(tools.NewEditFileTool(e.policy))
	registry.Register(tools.NewListDirTool(e.policy))
	registry.Register(tools.NewExecTool(e.policy, time.Duration(cfg.Agents.Defaults.ToolTimeoutSec)*time.Second))
	registry.Register(tools.NewWebSearchTool(cfg.Tools.Web.Search.APIKey, cfg.Tools.Web.Search.MaxResults))
	registry.Register(tools.NewWebFetchTool(50000))

	messageTool := tools.NewMessageTool(func(ctx context.Context, channel, chatID, content string) error {
		e.send(channel, chatID, content, map[string]interface{}{"session_id": msg.SessionID, "source": "tool:message"})
		return nil
	})
	messageTool.SetContext(msg.Channel, msg.ChatID, msg.SessionID)
	registry.Register(messageTool)

	spawnTool := tools.NewSpawnTool(e.spawnSubtask)
	spawnTool.SetContext(msg.SessionID, msg.Channel, msg.ChatID, msg.SenderID, subagentDepthFromMetadata(msg.Metadata))
	registry.Register(spawnTool)
	waitTool := tools.NewSubagentWaitTool(e.waitSubtasks)
	waitTool.SetContext(msg.SessionID)
	registry.Register(waitTool)

	statusTool := tools.NewSubagentStatusTool(e.statusSubtask)
	statusTool.SetContext(msg.SessionID)
	registry.Register(statusTool)

	resultTool := tools.NewSubagentResultTool(e.resultSubtask)
	resultTool.SetContext(msg.SessionID)
	registry.Register(resultTool)

	cancelTool := tools.NewSubagentCancelTool(e.cancelSubtask)
	cancelTool.SetContext(msg.SessionID)
	registry.Register(cancelTool)

	budgetStatusTool := tools.NewBudgetStatusTool(e.budgetStatus)
	budgetStatusTool.SetContext(msg.SessionID, msg.Channel, msg.SenderID)
	registry.Register(budgetStatusTool)

	budgetSetLimitsTool := tools.NewBudgetSetLimitsTool(e.budgetSetLimits)
	budgetSetLimitsTool.SetContext(msg.SessionID, msg.Channel, msg.SenderID)
	registry.Register(budgetSetLimitsTool)

	budgetSetModeTool := tools.NewBudgetSetModeTool(e.budgetSetMode)
	budgetSetModeTool.SetContext(msg.SessionID, msg.Channel, msg.SenderID)
	registry.Register(budgetSetModeTool)

	budgetSetEnabledTool := tools.NewBudgetSetEnabledTool(e.budgetSetEnabled)
	budgetSetEnabledTool.SetContext(msg.SessionID, msg.Channel, msg.SenderID)
	registry.Register(budgetSetEnabledTool)

	budgetSetEstimationTool := tools.NewBudgetSetEstimationTool(e.budgetSetEstimation)
	budgetSetEstimationTool.SetContext(msg.SessionID, msg.Channel, msg.SenderID)
	registry.Register(budgetSetEstimationTool)

	createTaskTool := tools.NewCreateTaskTool(e.createMissionTask)
	createTaskTool.SetContext(msg.SessionID, msg.Channel, msg.ChatID, msg.RequestID, msg.SenderID)
	registry.Register(createTaskTool)

	updateTaskTool := tools.NewUpdateTaskTool(e.updateMissionTask)
	updateTaskTool.SetContext(msg.SessionID, msg.Channel, msg.ChatID, msg.RequestID, msg.SenderID)
	registry.Register(updateTaskTool)

	return registry, nil
}

func subagentDepthFromMetadata(metadata map[string]any) int {
	if len(metadata) == 0 {
		return 0
	}
	raw, ok := metadata["subagent_depth"]
	if !ok || raw == nil {
		return 0
	}
	switch value := raw.(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		parsed, err := value.Int64()
		if err == nil {
			return int(parsed)
		}
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil {
			return parsed
		}
	}
	return 0
}

func (e *Engine) spawnSubtask(ctx context.Context, req tools.SpawnRequest) (tools.SpawnResponse, error) {
	if e.subagents == nil {
		return tools.SpawnResponse{}, fmt.Errorf("subagent manager is not configured")
	}
	cfg := e.currentConfig()
	workspace := config.WorkspacePath(cfg)
	taskID := e.nextID()
	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = req.Task
		if len(label) > 40 {
			label = label[:40] + "..."
		}
	}

	packet, err := e.buildSubagentContextPacket(ctx, req)
	if err != nil {
		return tools.SpawnResponse{}, err
	}
	artifactDir := filepath.Join(workspace, ".squidbot", "subagents", taskID)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return tools.SpawnResponse{}, err
	}
	run, err := e.subagents.Enqueue(ctx, subagent.Request{
		ID:               taskID,
		SessionID:        req.SessionID,
		Channel:          req.Channel,
		ChatID:           req.ChatID,
		SenderID:         req.SenderID,
		Task:             req.Task,
		Label:            label,
		ContextMode:      req.ContextMode,
		Attachments:      req.Attachments,
		TimeoutSec:       req.TimeoutSec,
		MaxAttempts:      req.MaxAttempts,
		Depth:            req.Depth + 1,
		NotifyOnComplete: cfg.Runtime.Subagents.NotifyOnComplete,
		ArtifactDir:      artifactDir,
		Context:          packet,
	})
	if err != nil {
		return tools.SpawnResponse{}, err
	}
	if req.Wait {
		waitTimeout := time.Duration(run.TimeoutSec+30) * time.Second
		if req.TimeoutSec > 0 {
			waitTimeout = time.Duration(req.TimeoutSec+30) * time.Second
		}
		waited, waitErr := e.subagents.Wait(ctx, []string{run.ID}, waitTimeout)
		if waitErr != nil && len(waited) == 0 {
			return tools.SpawnResponse{}, waitErr
		}
		if len(waited) > 0 {
			run = waited[0]
		}
		text := fmt.Sprintf("Subagent [%s] finished with status %s (run_id: %s).", label, run.Status, run.ID)
		if run.Result != nil && strings.TrimSpace(run.Result.Summary) != "" {
			text += "\n" + run.Result.Summary
		}
		if waitErr != nil {
			text += "\nWait interrupted: " + waitErr.Error()
		}
		return tools.SpawnResponse{RunID: run.ID, Status: run.Status, Result: run.Result, Text: text}, nil
	}
	return tools.SpawnResponse{
		RunID:  run.ID,
		Status: run.Status,
		Text:   fmt.Sprintf("Subagent [%s] started (run_id: %s). I will notify on completion.", label, run.ID),
	}, nil
}

func (e *Engine) waitSubtasks(ctx context.Context, req tools.SubagentWaitRequest) (tools.SubagentWaitResponse, error) {
	if e.subagents == nil {
		return tools.SubagentWaitResponse{}, fmt.Errorf("subagent manager is not configured")
	}
	for _, runID := range req.RunIDs {
		run, err := e.subagents.Status(ctx, runID)
		if err != nil {
			return tools.SubagentWaitResponse{}, err
		}
		if err := ensureSubagentRunAccess(req.SessionID, run); err != nil {
			return tools.SubagentWaitResponse{}, err
		}
	}
	timeout := time.Duration(req.TimeoutSec) * time.Second
	runs, err := e.subagents.Wait(ctx, req.RunIDs, timeout)
	if err != nil {
		return tools.SubagentWaitResponse{}, err
	}
	return tools.SubagentWaitResponse{Runs: runs}, nil
}

func (e *Engine) statusSubtask(ctx context.Context, req tools.SubagentStatusRequest) (tools.SubagentStatusResponse, error) {
	if e.subagents == nil {
		return tools.SubagentStatusResponse{}, fmt.Errorf("subagent manager is not configured")
	}
	run, err := e.subagents.Status(ctx, req.RunID)
	if err != nil {
		return tools.SubagentStatusResponse{}, err
	}
	if err := ensureSubagentRunAccess(req.SessionID, run); err != nil {
		return tools.SubagentStatusResponse{}, err
	}
	return tools.SubagentStatusResponse{Run: run}, nil
}

func (e *Engine) resultSubtask(ctx context.Context, req tools.SubagentResultRequest) (tools.SubagentResultResponse, error) {
	if e.subagents == nil {
		return tools.SubagentResultResponse{}, fmt.Errorf("subagent manager is not configured")
	}
	run, err := e.subagents.Result(ctx, req.RunID)
	if err != nil {
		return tools.SubagentResultResponse{}, err
	}
	if err := ensureSubagentRunAccess(req.SessionID, run); err != nil {
		return tools.SubagentResultResponse{}, err
	}
	result := tools.SubagentResultResponse{
		Status:  string(run.Status),
		Attempt: run.Attempt,
	}
	if run.Result != nil {
		result.Summary = run.Result.Summary
		result.Output = run.Result.Output
		result.ArtifactPaths = run.Result.ArtifactPaths
	}
	return result, nil
}

func (e *Engine) cancelSubtask(ctx context.Context, req tools.SubagentCancelRequest) (tools.SubagentCancelResponse, error) {
	if e.subagents == nil {
		return tools.SubagentCancelResponse{}, fmt.Errorf("subagent manager is not configured")
	}
	current, err := e.subagents.Status(ctx, req.RunID)
	if err != nil {
		return tools.SubagentCancelResponse{}, err
	}
	if err := ensureSubagentRunAccess(req.SessionID, current); err != nil {
		return tools.SubagentCancelResponse{}, err
	}
	run, err := e.subagents.Cancel(ctx, req.RunID)
	if err != nil {
		return tools.SubagentCancelResponse{}, err
	}
	return tools.SubagentCancelResponse{RunID: run.ID, Status: string(run.Status)}, nil
}

func ensureSubagentRunAccess(sessionID string, run subagent.Run) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	if strings.TrimSpace(run.SessionID) == sessionID {
		return nil
	}
	return fmt.Errorf("subagent run %s does not belong to session %s", run.ID, sessionID)
}

func (e *Engine) budgetStatus(ctx context.Context, req tools.BudgetStatusRequest) (tools.BudgetStatusResponse, error) {
	settings := e.effectiveTokenSafety(ctx)
	scopes := []string{"global"}
	if sessionID := strings.TrimSpace(req.SessionID); sessionID != "" {
		scopes = append(scopes, "session:"+sessionID)
	}
	if runID := strings.TrimSpace(req.RunID); runID != "" {
		scopes = append(scopes, "subagent:"+runID)
	}
	out := tools.BudgetStatusResponse{Settings: settings, Scopes: make([]tools.BudgetStatusScope, 0, len(scopes))}
	for _, scope := range scopes {
		counter, err := e.store.GetBudgetCounter(ctx, scope)
		if err != nil && !errors.Is(err, budget.ErrNotFound) {
			return tools.BudgetStatusResponse{}, err
		}
		hard, soft := tokenScopeLimit(scope, settings)
		used := counter.TotalTokens
		reserved := counter.ReservedTokens
		softWarning := hard > 0 && soft > 0 && (used+reserved)*100 >= hard*uint64(soft)
		hardExceeded := hard > 0 && used+reserved >= hard
		out.Scopes = append(out.Scopes, tools.BudgetStatusScope{
			Scope:        scope,
			Used:         used,
			Reserved:     reserved,
			HardLimit:    hard,
			WarningLevel: soft,
			SoftWarning:  softWarning,
			HardExceeded: hardExceeded,
		})
	}
	return out, nil
}

func (e *Engine) budgetSetLimits(ctx context.Context, req tools.BudgetSetLimitsRequest) (tools.BudgetSetLimitsResponse, error) {
	settings, err := e.updateTokenSafetySettings(ctx, req.Channel, req.SenderID, func(current *budget.Settings) error {
		if req.GlobalHardLimitTokens != nil {
			current.GlobalHardLimitTokens = *req.GlobalHardLimitTokens
		}
		if req.GlobalSoftThresholdPct != nil {
			current.GlobalSoftThresholdPct = *req.GlobalSoftThresholdPct
		}
		if req.SessionHardLimitTokens != nil {
			current.SessionHardLimitTokens = *req.SessionHardLimitTokens
		}
		if req.SessionSoftThresholdPct != nil {
			current.SessionSoftThresholdPct = *req.SessionSoftThresholdPct
		}
		if req.SubagentRunHardLimitTokens != nil {
			current.SubagentRunHardLimitTokens = *req.SubagentRunHardLimitTokens
		}
		if req.SubagentRunSoftThresholdPct != nil {
			current.SubagentRunSoftThresholdPct = *req.SubagentRunSoftThresholdPct
		}
		return nil
	})
	if err != nil {
		return tools.BudgetSetLimitsResponse{}, err
	}
	return tools.BudgetSetLimitsResponse{Settings: settings}, nil
}

func (e *Engine) budgetSetMode(ctx context.Context, req tools.BudgetSetModeRequest) (tools.BudgetSetModeResponse, error) {
	settings, err := e.updateTokenSafetySettings(ctx, req.Channel, req.SenderID, func(current *budget.Settings) error {
		current.Mode = budget.Mode(budget.NormalizeMode(req.Mode))
		return nil
	})
	if err != nil {
		return tools.BudgetSetModeResponse{}, err
	}
	return tools.BudgetSetModeResponse{Settings: settings}, nil
}

func (e *Engine) budgetSetEnabled(ctx context.Context, req tools.BudgetSetEnabledRequest) (tools.BudgetSetEnabledResponse, error) {
	settings, err := e.updateTokenSafetySettings(ctx, req.Channel, req.SenderID, func(current *budget.Settings) error {
		current.Enabled = req.Enabled
		return nil
	})
	if err != nil {
		return tools.BudgetSetEnabledResponse{}, err
	}
	return tools.BudgetSetEnabledResponse{Settings: settings}, nil
}

func (e *Engine) budgetSetEstimation(ctx context.Context, req tools.BudgetSetEstimationRequest) (tools.BudgetSetEstimationResponse, error) {
	settings, err := e.updateTokenSafetySettings(ctx, req.Channel, req.SenderID, func(current *budget.Settings) error {
		if req.EstimateOnMissingUsage != nil {
			current.EstimateOnMissingUsage = *req.EstimateOnMissingUsage
		}
		if req.EstimateCharsPerToken != nil {
			current.EstimateCharsPerToken = *req.EstimateCharsPerToken
		}
		return nil
	})
	if err != nil {
		return tools.BudgetSetEstimationResponse{}, err
	}
	return tools.BudgetSetEstimationResponse{Settings: settings}, nil
}

func (e *Engine) updateTokenSafetySettings(ctx context.Context, channel, senderID string, mutate func(current *budget.Settings) error) (budget.Settings, error) {
	if err := e.assertTrustedBudgetWriter(ctx, channel, senderID); err != nil {
		return budget.Settings{}, err
	}
	current := e.effectiveTokenSafety(ctx)
	if mutate != nil {
		if err := mutate(&current); err != nil {
			return budget.Settings{}, err
		}
	}
	current = current.Normalized()
	override := budget.TokenSafetyOverride{
		Settings:  current,
		UpdatedAt: time.Now().UTC(),
		Version:   1,
	}
	if err := e.store.PutTokenSafetyOverride(ctx, override); err != nil {
		return budget.Settings{}, err
	}
	e.invalidateTokenSafetyCache()
	return current, nil
}

func (e *Engine) assertTrustedBudgetWriter(ctx context.Context, channel, senderID string) error {
	if e.isTrustedBudgetWriter(ctx, channel, senderID) {
		return nil
	}
	return fmt.Errorf("not authorized to modify token safety settings")
}

func (e *Engine) isTrustedBudgetWriter(ctx context.Context, channel, senderID string) bool {
	channel = strings.ToLower(strings.TrimSpace(channel))
	senderID = strings.ToLower(strings.TrimSpace(senderID))
	if channel == "" || senderID == "" {
		return false
	}
	settings := e.effectiveTokenSafety(ctx)
	for _, raw := range settings.TrustedWriters {
		entry := strings.ToLower(strings.TrimSpace(raw))
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if parts[1] != senderID {
			continue
		}
		if parts[0] == "*" || parts[0] == channel {
			return true
		}
	}
	return false
}

func (e *Engine) effectiveTokenSafety(ctx context.Context) budget.Settings {
	now := time.Now()
	e.tokenSafetyMu.Lock()
	if e.tokenSafetyCachedAt.Add(e.tokenSafetyCacheTTL).After(now) {
		cached := e.tokenSafetyCached
		e.tokenSafetyMu.Unlock()
		return cached
	}
	e.tokenSafetyMu.Unlock()

	cfg := e.currentConfig()
	settings := tokenSafetySettingsFromConfig(cfg.Runtime.TokenSafety).Normalized()
	override, err := e.store.GetTokenSafetyOverride(ctx)
	if err == nil {
		settings = override.Settings.Normalized()
	} else if err != nil && !errors.Is(err, budget.ErrNotFound) {
		e.log.Printf("failed to load token safety override: %v", err)
	}

	e.tokenSafetyMu.Lock()
	e.tokenSafetyCached = settings
	e.tokenSafetyCachedAt = now
	e.tokenSafetyMu.Unlock()
	return settings
}

func (e *Engine) invalidateTokenSafetyCache() {
	e.tokenSafetyMu.Lock()
	e.tokenSafetyCachedAt = time.Time{}
	e.tokenSafetyCached = budget.Settings{}
	e.tokenSafetyMu.Unlock()
}

func tokenSafetySettingsFromConfig(cfg config.TokenSafetyRuntimeConfig) budget.Settings {
	return budget.Settings{
		Enabled:                     cfg.Enabled,
		Mode:                        budget.Mode(budget.NormalizeMode(cfg.Mode)),
		GlobalHardLimitTokens:       cfg.GlobalHardLimitTokens,
		GlobalSoftThresholdPct:      cfg.GlobalSoftThresholdPct,
		SessionHardLimitTokens:      cfg.SessionHardLimitTokens,
		SessionSoftThresholdPct:     cfg.SessionSoftThresholdPct,
		SubagentRunHardLimitTokens:  cfg.SubagentRunHardLimitTokens,
		SubagentRunSoftThresholdPct: cfg.SubagentRunSoftThresholdPct,
		EstimateOnMissingUsage:      cfg.EstimateOnMissingUsage,
		EstimateCharsPerToken:       cfg.EstimateCharsPerToken,
		TrustedWriters:              append([]string(nil), cfg.TrustedWriters...),
		ReservationTTLSec:           300,
	}
}

func tokenScopeLimit(scope string, settings budget.Settings) (hard uint64, soft int) {
	switch {
	case scope == "global":
		return settings.GlobalHardLimitTokens, settings.GlobalSoftThresholdPct
	case strings.HasPrefix(scope, "session:"):
		return settings.SessionHardLimitTokens, settings.SessionSoftThresholdPct
	case strings.HasPrefix(scope, "subagent:"):
		return settings.SubagentRunHardLimitTokens, settings.SubagentRunSoftThresholdPct
	default:
		return 0, 0
	}
}

func (e *Engine) buildSubagentContextPacket(ctx context.Context, req tools.SpawnRequest) (subagent.ContextPacket, error) {
	mode := req.ContextMode
	if mode == "" {
		mode = subagent.ContextModeMinimal
	}
	cfg := e.currentConfig()
	workspace := config.WorkspacePath(cfg)
	packet := subagent.ContextPacket{
		Mode:      mode,
		CreatedAt: time.Now().UTC(),
	}
	packet.SystemPrompt = "You are a background subagent. Complete only the assigned task and return a concise summary."
	if mode == subagent.ContextModeSession {
		packet.SystemPrompt += " Use the supplied parent session context."
	}
	if mode == subagent.ContextModeSessionMemory {
		packet.SystemPrompt = buildSystemPrompt(cfg, req.Task)
	}
	if req.SessionID != "" && mode != subagent.ContextModeMinimal {
		history, err := e.store.Window(ctx, req.SessionID, 24)
		if err == nil {
			packet.History = history
		}
	}
	for _, attachment := range req.Attachments {
		resolved, err := e.policy.Resolve(attachment)
		if err != nil {
			return subagent.ContextPacket{}, err
		}
		packet.Attachments = append(packet.Attachments, resolved)
	}
	if mode == subagent.ContextModeSessionMemory && e.memory != nil && e.memory.Enabled() {
		chunks, err := e.memory.Search(ctx, req.Task, minInt(6, cfg.Memory.TopK))
		if err == nil {
			for _, chunk := range chunks {
				packet.MemorySnippets = append(packet.MemorySnippets, fmt.Sprintf("%s: %s", shortPath(workspace, chunk.Path), truncateText(chunk.Content, 240)))
			}
		}
	}
	checksumPayload, _ := json.Marshal(map[string]any{
		"mode":            packet.Mode,
		"system_prompt":   packet.SystemPrompt,
		"history_len":     len(packet.History),
		"memory_snippets": packet.MemorySnippets,
		"attachments":     packet.Attachments,
		"task":            strings.TrimSpace(req.Task),
	})
	sum := sha1.Sum(checksumPayload)
	packet.Checksum = hex.EncodeToString(sum[:])
	return packet, nil
}

func (e *Engine) notifySubagentCompletion(run subagent.Run) {
	if strings.TrimSpace(run.Channel) == "" || strings.TrimSpace(run.ChatID) == "" {
		return
	}
	cfg := e.currentConfig()
	lines := []string{
		"[Subagent completed]",
		"",
		fmt.Sprintf("Run: %s", run.ID),
		fmt.Sprintf("Status: %s", run.Status),
		fmt.Sprintf("Task: %s", run.Task),
	}
	if run.Result != nil && strings.TrimSpace(run.Result.Summary) != "" {
		lines = append(lines, "", "Summary:", run.Result.Summary)
	}
	if strings.TrimSpace(run.Error) != "" {
		lines = append(lines, "", "Error:", run.Error)
	}
	e.send(run.Channel, run.ChatID, strings.Join(lines, "\n"), map[string]interface{}{
		"session_id":     run.SessionID,
		"source":         "subagent",
		"run_id":         run.ID,
		"status":         run.Status,
		"subagent_depth": run.Depth,
	})
	if cfg.Runtime.Subagents.ReinjectCompletion {
		_, err := e.Submit(context.Background(), InboundMessage{
			RequestID: e.nextID(),
			SessionID: run.SessionID,
			Channel:   run.Channel,
			ChatID:    run.ChatID,
			SenderID:  "subagent",
			Content:   strings.Join(lines, "\n"),
			CreatedAt: time.Now().UTC(),
			Metadata: map[string]any{
				"source":         "subagent_reinjected",
				"run_id":         run.ID,
				"status":         run.Status,
				"subagent_depth": run.Depth,
			},
		})
		if err != nil {
			e.log.Printf("failed to reinject subagent completion: %v", err)
		}
	}
}

func (e *Engine) runSubtask(ctx context.Context, run subagent.Run) (subagent.Result, error) {
	cfg := e.currentConfig()
	systemPrompt := strings.TrimSpace(run.Context.SystemPrompt)
	if systemPrompt == "" {
		systemPrompt = "You are a background subagent. Complete only the assigned task and return a concise summary."
	}
	messages := []provider.Message{
		{Role: "system", Content: systemPrompt},
	}
	if len(run.Context.History) > 0 {
		messages = append(messages, run.Context.History...)
	}
	if len(run.Context.MemorySnippets) > 0 {
		messages = append(messages, provider.Message{Role: "user", Content: "Relevant memory:\n- " + strings.Join(run.Context.MemorySnippets, "\n- ")})
	}
	if len(run.Context.Attachments) > 0 {
		messages = append(messages, provider.Message{Role: "user", Content: "Attachment paths available in workspace:\n- " + strings.Join(run.Context.Attachments, "\n- ")})
	}
	messages = append(messages, provider.Message{Role: "user", Content: run.Task})
	registry := tools.NewRegistry()
	registry.Register(tools.NewReadFileTool(e.policy))
	if cfg.Runtime.Subagents.AllowWrites {
		registry.Register(tools.NewWriteFileTool(e.policy))
		registry.Register(tools.NewEditFileTool(e.policy))
	}
	registry.Register(tools.NewListDirTool(e.policy))
	registry.Register(tools.NewExecTool(e.policy, time.Duration(cfg.Agents.Defaults.ToolTimeoutSec)*time.Second))
	registry.Register(tools.NewWebSearchTool(cfg.Tools.Web.Search.APIKey, cfg.Tools.Web.Search.MaxResults))
	registry.Register(tools.NewWebFetchTool(30000))

	maxHops := cfg.Agents.Defaults.MaxToolIterations
	if maxHops <= 0 {
		maxHops = 15
	}
	finalContent := ""
	budgetWarnings := []string{}
	for i := 0; i < maxHops; i++ {
		settings := e.effectiveTokenSafety(ctx)
		scopeLimits := []budget.ScopeLimit{
			{Key: "global", HardLimitTokens: settings.GlobalHardLimitTokens, SoftThresholdPct: settings.GlobalSoftThresholdPct},
		}
		if strings.TrimSpace(run.SessionID) != "" {
			scopeLimits = append(scopeLimits, budget.ScopeLimit{
				Key:              "session:" + strings.TrimSpace(run.SessionID),
				HardLimitTokens:  settings.SessionHardLimitTokens,
				SoftThresholdPct: settings.SessionSoftThresholdPct,
			})
		}
		scopeLimits = append(scopeLimits, budget.ScopeLimit{
			Key:              "subagent:" + strings.TrimSpace(run.ID),
			HardLimitTokens:  settings.SubagentRunHardLimitTokens,
			SoftThresholdPct: settings.SubagentRunSoftThresholdPct,
		})
		preflight, preflightErr := e.budgetGuard.Preflight(ctx, settings, scopeLimits, uint64(max(cfg.Agents.Defaults.MaxTokens, 1)))
		if preflightErr != nil {
			var limitErr *budget.LimitError
			if errors.As(preflightErr, &limitErr) {
				return subagent.Result{}, fmt.Errorf("budget_exhausted: %w", limitErr)
			}
			return subagent.Result{}, preflightErr
		}
		e.metrics.ProviderCalls.Add(1)
		providerClient, model := e.currentProviderModel()
		resp, err := providerClient.Chat(ctx, provider.ChatRequest{
			Messages:    messages,
			Tools:       registry.Definitions(),
			Model:       model,
			MaxTokens:   cfg.Agents.Defaults.MaxTokens,
			Temperature: cfg.Agents.Defaults.Temperature,
		})
		if err != nil {
			e.budgetGuard.Abort(ctx, preflight)
			e.metrics.ProviderErrors.Add(1)
			return subagent.Result{}, err
		}
		commit, commitErr := e.budgetGuard.Commit(ctx, settings, scopeLimits, preflight, budget.Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
			OutputChars:      len(resp.Content),
		})
		if commitErr != nil {
			e.log.Printf("failed to commit subagent token budget usage: %v", commitErr)
		}
		e.recordUsageDay(ctx,
			uint64(max(resp.Usage.PromptTokens, 0)),
			uint64(max(resp.Usage.CompletionTokens, 0)),
			commit.TotalTokens,
		)
		for _, warning := range commit.Warnings {
			budgetWarnings = append(budgetWarnings, fmt.Sprintf("%s at %d%% of hard limit", warning.Scope, warning.ThresholdPct))
		}
		if resp.HasToolCalls() {
			messages = append(messages, provider.Message{Role: "assistant", Content: resp.Content, ToolCalls: resp.ToolCalls})
			for _, tc := range resp.ToolCalls {
				e.metrics.ToolCalls.Add(1)
				result, toolErr := registry.Execute(ctx, tc.Name, tc.Arguments)
				if toolErr != nil {
					e.metrics.ToolErrors.Add(1)
					result = tools.ToolResult{Text: toolErr.Error()}
				}
				messages = append(messages, provider.Message{Role: "tool", ToolCallID: tc.ID, Name: tc.Name, Content: result.Text})
			}
			continue
		}
		finalContent = strings.TrimSpace(resp.Content)
		if finalContent == "" {
			finalContent = "Task completed."
		}
		break
	}
	if strings.TrimSpace(finalContent) == "" {
		finalContent = "Task completed but no final response was generated."
	}
	if len(budgetWarnings) > 0 {
		finalContent = strings.TrimSpace(finalContent) + "\n\n[Token safety]\n- " + strings.Join(budgetWarnings, "\n- ")
	}
	artifactPaths := []string{}
	if strings.TrimSpace(run.ArtifactDir) != "" {
		if err := os.MkdirAll(run.ArtifactDir, 0o755); err != nil {
			return subagent.Result{}, err
		}
		resultPath := filepath.Join(run.ArtifactDir, "result.txt")
		if err := os.WriteFile(resultPath, []byte(finalContent), 0o644); err != nil {
			return subagent.Result{}, err
		}
		artifactPaths = append(artifactPaths, resultPath)
		contextPath := filepath.Join(run.ArtifactDir, "context.json")
		if data, err := json.MarshalIndent(run.Context, "", "  "); err == nil {
			if err := os.WriteFile(contextPath, data, 0o644); err == nil {
				artifactPaths = append(artifactPaths, contextPath)
			}
		}
	}
	summary := finalContent
	if len(summary) > 240 {
		summary = summary[:237] + "..."
	}
	return subagent.Result{Summary: summary, Output: finalContent, ArtifactPaths: artifactPaths}, nil
}

func (e *Engine) createMissionTask(ctx context.Context, req tools.CreateTaskRequest) (tools.TaskResult, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return tools.TaskResult{}, fmt.Errorf("title is required")
	}
	source := mission.TaskSource{
		Type:      sourceTypeFromContext(req.Channel, req.SessionID, req.Trigger),
		SessionID: req.SessionID,
		Channel:   req.Channel,
		ChatID:    req.ChatID,
		RequestID: req.RequestID,
		Trigger:   req.Trigger,
	}
	policy, err := e.store.GetTaskAutomationPolicy(ctx)
	if err != nil {
		return tools.TaskResult{}, err
	}
	if !policy.EnabledForSource(source.Type) {
		return tools.TaskResult{}, fmt.Errorf("task creation disabled for source %q", source.Type)
	}

	columns, err := e.ensureMissionColumns(ctx)
	if err != nil {
		return tools.TaskResult{}, err
	}
	columnID := strings.TrimSpace(req.ColumnID)
	if columnID == "" {
		columnID = strings.TrimSpace(policy.DefaultColumnID)
		if columnID == "" {
			columnID = mission.ColumnBacklog
		}
	}
	if _, ok := columns[columnID]; !ok {
		columnID = mission.ColumnBacklog
	}

	now := time.Now().UTC()
	normalized := mission.NormalizeTaskTitle(title)
	all, err := e.store.ListMissionTasks(ctx)
	if err != nil {
		return tools.TaskResult{}, err
	}

	dedupeWindow := policy.DedupeWindow()
	for _, candidate := range all {
		if mission.NormalizeTaskTitle(candidate.Title) != normalized {
			continue
		}
		if candidate.Source.Type != source.Type {
			continue
		}
		if source.SessionID != "" && candidate.Source.SessionID != source.SessionID {
			continue
		}
		if now.Sub(candidate.CreatedAt) > dedupeWindow {
			continue
		}
		candidate.UpdatedAt = now
		candidate.Version++
		if strings.TrimSpace(req.Description) != "" {
			candidate.Description = strings.TrimSpace(req.Description)
		}
		if priority := mission.NormalizePriority(req.Priority); priority != "" {
			candidate.Priority = priority
		}
		if strings.TrimSpace(req.Assignee) != "" {
			candidate.Assignee = strings.TrimSpace(req.Assignee)
		}
		if strings.TrimSpace(req.Notes) != "" {
			if strings.TrimSpace(candidate.Notes) == "" {
				candidate.Notes = strings.TrimSpace(req.Notes)
			} else {
				candidate.Notes = strings.TrimSpace(candidate.Notes) + "\n" + strings.TrimSpace(req.Notes)
			}
		}
		if req.DueAt != nil {
			due := req.DueAt.UTC()
			candidate.DueAt = &due
		}
		if columnID != "" && candidate.ColumnID != columnID {
			candidate.ColumnID = columnID
			candidate.Position = e.nextTaskPosition(all, columnID)
			candidate.Events = append(candidate.Events, mission.TaskEvent{
				ID:        e.nextID(),
				Type:      mission.TaskEventMoved,
				Actor:     "agent",
				Summary:   "task moved by dedupe update",
				CreatedAt: now,
			})
		}
		candidate.Events = append(candidate.Events, mission.TaskEvent{
			ID:        e.nextID(),
			Type:      mission.TaskEventUpdated,
			Actor:     "agent",
			Summary:   "task updated by dedupe",
			CreatedAt: now,
		})
		if err := e.store.PutMissionTask(ctx, candidate); err != nil {
			return tools.TaskResult{}, err
		}
		return tools.TaskResult{ID: candidate.ID, ColumnID: candidate.ColumnID, Updated: true}, nil
	}

	task := mission.Task{
		ID:          e.nextID(),
		Title:       title,
		Description: strings.TrimSpace(req.Description),
		ColumnID:    columnID,
		Priority:    mission.NormalizePriority(req.Priority),
		Assignee:    strings.TrimSpace(req.Assignee),
		Notes:       strings.TrimSpace(req.Notes),
		Source:      source,
		Position:    e.nextTaskPosition(all, columnID),
		CreatedAt:   now,
		UpdatedAt:   now,
		Version:     1,
		Events: []mission.TaskEvent{{
			ID:        e.nextID(),
			Type:      mission.TaskEventCreated,
			Actor:     "agent",
			Summary:   "task created by agent",
			CreatedAt: now,
		}},
	}
	if req.DueAt != nil {
		due := req.DueAt.UTC()
		task.DueAt = &due
	}
	if err := e.store.PutMissionTask(ctx, task); err != nil {
		return tools.TaskResult{}, err
	}
	return tools.TaskResult{ID: task.ID, ColumnID: task.ColumnID, Updated: false}, nil
}

func (e *Engine) updateMissionTask(ctx context.Context, req tools.UpdateTaskRequest) (tools.TaskResult, error) {
	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		return tools.TaskResult{}, fmt.Errorf("task_id is required")
	}
	all, err := e.store.ListMissionTasks(ctx)
	if err != nil {
		return tools.TaskResult{}, err
	}
	idx := -1
	for i := range all {
		if all[i].ID == taskID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return tools.TaskResult{}, fmt.Errorf("task not found")
	}
	columns, err := e.ensureMissionColumns(ctx)
	if err != nil {
		return tools.TaskResult{}, err
	}
	now := time.Now().UTC()
	task := all[idx]
	if title := strings.TrimSpace(req.Title); title != "" {
		task.Title = title
	}
	if desc := strings.TrimSpace(req.Description); desc != "" {
		task.Description = desc
	}
	if priority := mission.NormalizePriority(req.Priority); priority != "" {
		task.Priority = priority
	}
	if assignee := strings.TrimSpace(req.Assignee); assignee != "" {
		task.Assignee = assignee
	}
	if notes := strings.TrimSpace(req.Notes); notes != "" {
		if strings.TrimSpace(task.Notes) == "" {
			task.Notes = notes
		} else {
			task.Notes = strings.TrimSpace(task.Notes) + "\n" + notes
		}
	}
	if req.DueAt != nil {
		due := req.DueAt.UTC()
		task.DueAt = &due
	}
	if column := strings.TrimSpace(req.ColumnID); column != "" {
		if _, ok := columns[column]; ok && task.ColumnID != column {
			task.ColumnID = column
			task.Position = e.nextTaskPosition(all, column)
			task.Events = append(task.Events, mission.TaskEvent{
				ID:        e.nextID(),
				Type:      mission.TaskEventMoved,
				Actor:     "agent",
				Summary:   "task moved by update",
				CreatedAt: now,
			})
		}
	}

	task.UpdatedAt = now
	task.Version++
	task.Events = append(task.Events, mission.TaskEvent{
		ID:        e.nextID(),
		Type:      mission.TaskEventUpdated,
		Actor:     "agent",
		Summary:   "task updated by agent",
		CreatedAt: now,
	})
	if err := e.store.PutMissionTask(ctx, task); err != nil {
		return tools.TaskResult{}, err
	}
	return tools.TaskResult{ID: task.ID, ColumnID: task.ColumnID, Updated: true}, nil
}

func (e *Engine) ensureMissionColumns(ctx context.Context) (map[string]mission.Column, error) {
	columns, err := e.store.ListMissionColumns(ctx)
	if err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		defaults := mission.DefaultColumns(time.Now().UTC())
		if err := e.store.ReplaceMissionColumns(ctx, defaults); err != nil {
			return nil, err
		}
		columns = defaults
	}
	out := make(map[string]mission.Column, len(columns))
	for _, column := range columns {
		out[column.ID] = column
	}
	return out, nil
}

func (e *Engine) nextTaskPosition(tasks []mission.Task, columnID string) int {
	maxPos := -1
	for _, task := range tasks {
		if task.ColumnID != columnID {
			continue
		}
		if task.Position > maxPos {
			maxPos = task.Position
		}
	}
	return maxPos + 1
}

func sourceTypeFromContext(channel, sessionID, trigger string) mission.TaskSourceType {
	trigger = strings.TrimSpace(strings.ToLower(trigger))
	if trigger == "subagent" {
		return mission.TaskSourceSubagent
	}
	switch strings.TrimSpace(strings.ToLower(channel)) {
	case "system":
		if strings.TrimSpace(strings.ToLower(sessionID)) == "system:heartbeat" {
			return mission.TaskSourceHeartbeat
		}
		return mission.TaskSourceSystem
	case "cron":
		return mission.TaskSourceCron
	case "api":
		return mission.TaskSourceAPI
	default:
		return mission.TaskSourceChat
	}
}

func (e *Engine) formatBudgetLimitMessage(limitErr *budget.LimitError) string {
	if limitErr == nil {
		return "Token safety blocked this request."
	}
	return fmt.Sprintf(
		"Token safety blocked this request for scope %s (used=%d reserved=%d requested=%d limit=%d). Adjust limits or disable token safety if this is expected.",
		limitErr.Scope,
		limitErr.Used,
		limitErr.Reserved,
		limitErr.Requested,
		limitErr.Limit,
	)
}

func (e *Engine) recordUsageDay(ctx context.Context, promptTokens, completionTokens, totalTokens uint64) {
	if promptTokens == 0 && completionTokens == 0 && totalTokens == 0 {
		return
	}
	day := time.Now().UTC().Format("2006-01-02")
	if err := e.store.RecordUsageDay(
		ctx,
		day,
		promptTokens,
		completionTokens,
		totalTokens,
	); err != nil {
		e.log.Printf("failed to record token usage: %v", err)
	}
}

func max(v, floor int) int {
	if v < floor {
		return floor
	}
	return v
}
