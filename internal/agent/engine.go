package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	mrand "math/rand"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/memory"
	"github.com/grixate/squidbot/internal/provider"
	"github.com/grixate/squidbot/internal/runtime/actor"
	"github.com/grixate/squidbot/internal/telemetry"
	"github.com/grixate/squidbot/internal/tools"
)

type Store interface {
	ConversationStore
	KVStore
	SchedulerStore
	ToolEventStore
	SaveCheckpoint(ctx context.Context, sessionID string, payload []byte) error
	LoadCheckpoint(ctx context.Context, sessionID string) ([]byte, error)
}

type Engine struct {
	cfg      config.Config
	provider provider.LLMProvider
	model    string
	store    Store
	metrics  *telemetry.Metrics
	log      *log.Logger
	actors   *actor.System
	outbound chan OutboundMessage
	policy   *tools.PathPolicy
	memory   *memory.Manager
	ulidMu   sync.Mutex
	entropy  *ulid.MonotonicEntropy
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
		cfg:      cfg,
		provider: providerClient,
		model:    model,
		store:    store,
		metrics:  metrics,
		log:      logger,
		outbound: make(chan OutboundMessage, 512),
		policy:   policy,
		memory:   memory.NewManager(cfg),
		entropy:  ulid.Monotonic(mrand.New(mrand.NewSource(time.Now().UnixNano())), 0),
	}
	system := actor.NewSystem(engine.newSessionHandler, cfg.Runtime.MailboxSize, cfg.Runtime.ActorIdleTTL.Duration)
	system.SetActorHooks(func() { engine.metrics.ActiveActors.Add(1) }, func() { engine.metrics.ActiveActors.Add(-1) })
	engine.actors = system
	return engine, nil
}

func (e *Engine) Outbound() <-chan OutboundMessage {
	return e.outbound
}

func (e *Engine) EmitOutbound(channel, chatID, content string, metadata map[string]interface{}) {
	e.send(channel, chatID, content, metadata)
}

func (e *Engine) Close() error {
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
	turnTimeout := time.Duration(h.engine.cfg.Agents.Defaults.TurnTimeoutSec) * time.Second
	if turnTimeout <= 0 {
		turnTimeout = 120 * time.Second
	}
	turnCtx, cancel := context.WithTimeout(ctx, turnTimeout)
	defer cancel()

	history, err := h.engine.store.Window(turnCtx, h.sessionID, 50)
	if err != nil {
		return "", err
	}
	systemPrompt := buildSystemPrompt(h.engine.cfg, msg.Content)
	messages := buildMessages(systemPrompt, history, msg.Content)
	registry, err := h.engine.buildRegistry(msg)
	if err != nil {
		return "", err
	}

	maxHops := h.engine.cfg.Agents.Defaults.MaxToolIterations
	if maxHops <= 0 {
		maxHops = 20
	}
	finalContent := ""

	for i := 0; i < maxHops; i++ {
		h.engine.metrics.ProviderCalls.Add(1)
		response, chatErr := h.engine.provider.Chat(turnCtx, provider.ChatRequest{
			Messages:    messages,
			Tools:       registry.Definitions(),
			Model:       h.engine.model,
			MaxTokens:   h.engine.cfg.Agents.Defaults.MaxTokens,
			Temperature: h.engine.cfg.Agents.Defaults.Temperature,
		})
		if chatErr != nil {
			h.engine.metrics.ProviderErrors.Add(1)
			return "", chatErr
		}

		if response.HasToolCalls() {
			messages = append(messages, provider.Message{Role: "assistant", Content: response.Content, ToolCalls: response.ToolCalls})
			for _, tc := range response.ToolCalls {
				h.engine.metrics.ToolCalls.Add(1)
				toolTimeout := time.Duration(h.engine.cfg.Agents.Defaults.ToolTimeoutSec) * time.Second
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
	registry := tools.NewRegistry()
	registry.Register(tools.NewReadFileTool(e.policy))
	registry.Register(tools.NewWriteFileTool(e.policy))
	registry.Register(tools.NewEditFileTool(e.policy))
	registry.Register(tools.NewListDirTool(e.policy))
	registry.Register(tools.NewExecTool(e.policy, time.Duration(e.cfg.Agents.Defaults.ToolTimeoutSec)*time.Second))
	registry.Register(tools.NewWebSearchTool(e.cfg.Tools.Web.Search.APIKey, e.cfg.Tools.Web.Search.MaxResults))
	registry.Register(tools.NewWebFetchTool(50000))

	messageTool := tools.NewMessageTool(func(ctx context.Context, channel, chatID, content string) error {
		e.send(channel, chatID, content, map[string]interface{}{"session_id": msg.SessionID, "source": "tool:message"})
		return nil
	})
	messageTool.SetContext(msg.Channel, msg.ChatID, msg.SessionID)
	registry.Register(messageTool)

	spawnTool := tools.NewSpawnTool(e.spawnSubtask)
	spawnTool.SetContext(msg.SessionID, msg.Channel, msg.ChatID, msg.SenderID)
	registry.Register(spawnTool)

	return registry, nil
}

func (e *Engine) spawnSubtask(ctx context.Context, req tools.SpawnRequest) (string, error) {
	taskID := e.nextID()
	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = req.Task
		if len(label) > 40 {
			label = label[:40] + "..."
		}
	}

	go func() {
		subCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		result, err := e.runSubtask(subCtx, req.Task)
		if err != nil {
			result = "Error: " + err.Error()
		}

		announce := fmt.Sprintf("[Background task completed]\n\nTask: %s\n\nResult:\n%s", req.Task, result)
		_, submitErr := e.Submit(context.Background(), InboundMessage{
			RequestID: e.nextID(),
			SessionID: req.SessionID,
			Channel:   req.Channel,
			ChatID:    req.ChatID,
			SenderID:  "subagent",
			Content:   announce,
			CreatedAt: time.Now().UTC(),
			Metadata:  map[string]interface{}{"task_id": taskID, "background": true},
		})
		if submitErr != nil {
			e.log.Printf("failed to submit subtask completion: %v", submitErr)
		}
	}()

	return fmt.Sprintf("Subagent [%s] started (id: %s). I'll notify you when it completes.", label, taskID), nil
}

func (e *Engine) runSubtask(ctx context.Context, task string) (string, error) {
	messages := []provider.Message{
		{Role: "system", Content: "You are a background subagent. Complete only the assigned task and return a concise summary."},
		{Role: "user", Content: task},
	}
	registry := tools.NewRegistry()
	registry.Register(tools.NewReadFileTool(e.policy))
	registry.Register(tools.NewWriteFileTool(e.policy))
	registry.Register(tools.NewListDirTool(e.policy))
	registry.Register(tools.NewExecTool(e.policy, time.Duration(e.cfg.Agents.Defaults.ToolTimeoutSec)*time.Second))
	registry.Register(tools.NewWebSearchTool(e.cfg.Tools.Web.Search.APIKey, e.cfg.Tools.Web.Search.MaxResults))
	registry.Register(tools.NewWebFetchTool(30000))

	for i := 0; i < 15; i++ {
		resp, err := e.provider.Chat(ctx, provider.ChatRequest{
			Messages:    messages,
			Tools:       registry.Definitions(),
			Model:       e.model,
			MaxTokens:   e.cfg.Agents.Defaults.MaxTokens,
			Temperature: e.cfg.Agents.Defaults.Temperature,
		})
		if err != nil {
			return "", err
		}
		if resp.HasToolCalls() {
			messages = append(messages, provider.Message{Role: "assistant", Content: resp.Content, ToolCalls: resp.ToolCalls})
			for _, tc := range resp.ToolCalls {
				result, toolErr := registry.Execute(ctx, tc.Name, tc.Arguments)
				if toolErr != nil {
					result = tools.ToolResult{Text: toolErr.Error()}
				}
				messages = append(messages, provider.Message{Role: "tool", ToolCallID: tc.ID, Name: tc.Name, Content: result.Text})
			}
			continue
		}
		if strings.TrimSpace(resp.Content) == "" {
			return "Task completed.", nil
		}
		return resp.Content, nil
	}
	return "Task completed but no final response was generated.", nil
}
