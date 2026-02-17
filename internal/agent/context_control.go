package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/contextctrl"
	"github.com/grixate/squidbot/internal/provider"
	"github.com/grixate/squidbot/internal/skills"
)

const contextSummaryNamespace = "context_summary"

type sessionSummaryRecord struct {
	Version          int       `json:"version"`
	UpdatedAt        time.Time `json:"updatedAt"`
	CoveredTurnCount int       `json:"coveredTurnCount"`
	Content          string    `json:"content"`
}

type contextBuildResult struct {
	Messages     []provider.Message
	Stage        contextctrl.Stage
	Profile      contextctrl.ModelWindow
	PromptTokens int
	HistoryTurns int
}

func (e *Engine) buildAdaptiveMessages(
	ctx context.Context,
	cfg config.Config,
	model string,
	sessionID string,
	userMessage string,
	activation skills.ActivationResult,
	stageOverride *contextctrl.Stage,
) (contextBuildResult, error) {
	maxHistory := max(cfg.ContextControl.MaxHistoryTurns, 1)
	if !cfg.ContextControl.Enabled {
		history, err := e.store.Window(ctx, sessionID, maxHistory)
		if err != nil {
			return contextBuildResult{}, err
		}
		systemPrompt := buildSystemPromptWithSkills(cfg, userMessage, &activation)
		messages := buildMessages(systemPrompt, history, userMessage)
		return contextBuildResult{
			Messages:     messages,
			Stage:        contextctrl.StageNone,
			Profile:      contextctrl.ModelWindow{ContextWindowTokens: cfg.Agents.Defaults.MaxTokens, OutputReserveTokens: 0, CharsPerToken: 4.0, Source: "disabled"},
			PromptTokens: contextctrl.EstimateMessagesTokens(messages, 4.0),
			HistoryTurns: len(history),
		}, nil
	}

	profile, err := e.resolveModelWindow(model)
	if err != nil {
		e.log.Printf("contextctrl registry fallback model=%s err=%v", model, err)
	}
	historyAll, err := e.store.Window(ctx, sessionID, maxHistory)
	if err != nil {
		return contextBuildResult{}, err
	}
	summary, _ := e.loadSessionSummary(ctx, sessionID)
	thresholds := contextctrl.Thresholds{
		Stage1Pct: cfg.ContextControl.Stage1Pct,
		Stage2Pct: cfg.ContextControl.Stage2Pct,
		Stage3Pct: cfg.ContextControl.Stage3Pct,
	}.Normalized()
	stage := contextctrl.StageNone
	if stageOverride != nil {
		stage = *stageOverride
	} else {
		baseOptions := e.promptBuildOptionsForStage(ctx, cfg, contextctrl.StageNone, summary.Content)
		baseMessages := buildMessages(
			buildSystemPromptWithSkillsAndOptions(cfg, userMessage, &activation, baseOptions),
			historyAll,
			userMessage,
		)
		stage = contextctrl.SelectStage(
			contextctrl.UtilizationPct(
				contextctrl.EstimateMessagesTokens(baseMessages, profile.CharsPerToken),
				profile.OutputReserveTokens,
				profile.ContextWindowTokens,
			),
			thresholds,
		)
	}

	historyLimit := e.historyLimitForStage(cfg, stage)
	if historyLimit > len(historyAll) {
		historyLimit = len(historyAll)
	}
	if historyLimit < 0 {
		historyLimit = 0
	}
	if stage >= contextctrl.Stage2 && historyLimit < len(historyAll) {
		dropped := append([]provider.Message(nil), historyAll[:len(historyAll)-historyLimit]...)
		updatedSummary, summaryErr := e.buildSessionSummary(ctx, cfg, model, sessionID, dropped, summary.Content)
		if summaryErr != nil {
			e.log.Printf("contextctrl summary fallback session=%s err=%v", sessionID, summaryErr)
		}
		if strings.TrimSpace(updatedSummary) != "" && strings.TrimSpace(updatedSummary) != strings.TrimSpace(summary.Content) {
			record := sessionSummaryRecord{
				Version:          1,
				UpdatedAt:        time.Now().UTC(),
				CoveredTurnCount: len(historyAll) - historyLimit,
				Content:          updatedSummary,
			}
			if err := e.saveSessionSummary(ctx, sessionID, record); err != nil {
				e.log.Printf("contextctrl summary save failed session=%s err=%v", sessionID, err)
			}
			summary = record
		}
	}
	options := e.promptBuildOptionsForStage(ctx, cfg, stage, summary.Content)
	history := lastMessages(historyAll, historyLimit)
	systemPrompt := buildSystemPromptWithSkillsAndOptions(cfg, userMessage, &activation, options)
	messages := buildMessages(systemPrompt, history, userMessage)
	promptTokens := contextctrl.EstimateMessagesTokens(messages, profile.CharsPerToken)

	if promptTokens+profile.OutputReserveTokens > profile.ContextWindowTokens {
		stage = contextctrl.Stage3
		historyLimit = e.historyLimitForStage(cfg, stage)
		if historyLimit > len(historyAll) {
			historyLimit = len(historyAll)
		}
		options = e.promptBuildOptionsForStage(ctx, cfg, stage, summary.Content)
		history = lastMessages(historyAll, historyLimit)
		systemPrompt = buildSystemPromptWithSkillsAndOptions(cfg, userMessage, &activation, options)
		messages = buildMessages(systemPrompt, history, userMessage)
		promptTokens = contextctrl.EstimateMessagesTokens(messages, profile.CharsPerToken)
		minHistory := max(cfg.ContextControl.MinHistoryTurns, 1)
		for historyLimit > minHistory && promptTokens+profile.OutputReserveTokens > profile.ContextWindowTokens {
			historyLimit--
			history = lastMessages(historyAll, historyLimit)
			messages = buildMessages(systemPrompt, history, userMessage)
			promptTokens = contextctrl.EstimateMessagesTokens(messages, profile.CharsPerToken)
		}
	}

	return contextBuildResult{
		Messages:     messages,
		Stage:        stage,
		Profile:      profile,
		PromptTokens: promptTokens,
		HistoryTurns: len(history),
	}, nil
}

func (e *Engine) resolveModelWindow(model string) (contextctrl.ModelWindow, error) {
	if e == nil || e.contextRegistry == nil {
		return contextctrl.ModelWindow{
			ContextWindowTokens: 8192,
			OutputReserveTokens: 1024,
			CharsPerToken:       4.0,
			Source:              "default",
		}, nil
	}
	return e.contextRegistry.Resolve(model)
}

func (e *Engine) historyLimitForStage(cfg config.Config, stage contextctrl.Stage) int {
	maxHistory := max(cfg.ContextControl.MaxHistoryTurns, 1)
	minHistory := max(cfg.ContextControl.MinHistoryTurns, 1)
	if minHistory > maxHistory {
		minHistory = maxHistory
	}
	switch stage {
	case contextctrl.Stage1:
		return max(minHistory+4, (maxHistory*2)/3)
	case contextctrl.Stage2:
		return max(minHistory+2, maxHistory/2)
	case contextctrl.Stage3:
		return minHistory
	default:
		return maxHistory
	}
}

func (e *Engine) promptBuildOptionsForStage(ctx context.Context, cfg config.Config, stage contextctrl.Stage, summary string) PromptBuildOptions {
	options := defaultPromptBuildOptions(cfg)
	options.BootstrapMaxChars = max(cfg.ContextControl.BootstrapMaxChars, 256)
	options.MemorySnippetMaxChars = max(cfg.ContextControl.MemorySnippetMaxChars, 80)
	options.SkillPromptMaxChars = max(cfg.ContextControl.SkillPromptMaxChars, 400)
	options.SessionSummary = summary
	options.Bulletin = e.currentCortexBulletin(ctx)
	switch stage {
	case contextctrl.Stage1:
		options.BootstrapMaxChars = max(options.BootstrapMaxChars*70/100, 256)
		options.MemorySnippetMaxChars = max(options.MemorySnippetMaxChars*70/100, 80)
		options.SkillPromptMaxChars = max(options.SkillPromptMaxChars*65/100, 400)
		options.TopK = max(cfg.Memory.TopK/2, 1)
		options.RecentDailyLimit = minInt(2, options.TopK)
	case contextctrl.Stage2:
		options.BootstrapMaxChars = max(options.BootstrapMaxChars*55/100, 220)
		options.MemorySnippetMaxChars = max(options.MemorySnippetMaxChars*55/100, 64)
		options.SkillPromptMaxChars = max(options.SkillPromptMaxChars*45/100, 300)
		options.TopK = 1
		options.RecentDailyLimit = 1
	case contextctrl.Stage3:
		options.BootstrapMaxChars = max(options.BootstrapMaxChars*40/100, 180)
		options.MemorySnippetMaxChars = max(options.MemorySnippetMaxChars*40/100, 48)
		options.SkillPromptMaxChars = max(options.SkillPromptMaxChars*30/100, 240)
		options.TopK = 1
		options.RecentDailyLimit = 1
		options.IncludeRetrievedMemory = false
		options.IncludeRecentDaily = false
	}
	return options
}

func (e *Engine) loadSessionSummary(ctx context.Context, sessionID string) (sessionSummaryRecord, error) {
	if e == nil || e.store == nil {
		return sessionSummaryRecord{}, fmt.Errorf("store is not configured")
	}
	raw, err := e.store.GetKV(ctx, contextSummaryNamespace, strings.TrimSpace(sessionID))
	if err != nil || len(raw) == 0 {
		return sessionSummaryRecord{}, err
	}
	var record sessionSummaryRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return sessionSummaryRecord{}, err
	}
	if strings.TrimSpace(record.Content) == "" {
		return sessionSummaryRecord{}, nil
	}
	return record, nil
}

func (e *Engine) saveSessionSummary(ctx context.Context, sessionID string, record sessionSummaryRecord) error {
	if e == nil || e.store == nil {
		return fmt.Errorf("store is not configured")
	}
	if strings.TrimSpace(record.Content) == "" {
		return nil
	}
	if record.Version <= 0 {
		record.Version = 1
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = time.Now().UTC()
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return e.store.PutKV(ctx, contextSummaryNamespace, strings.TrimSpace(sessionID), raw)
}

func (e *Engine) buildSessionSummary(ctx context.Context, cfg config.Config, model, sessionID string, droppedTurns []provider.Message, previousSummary string) (string, error) {
	limitInput := max(cfg.ContextControl.Summary.MaxInputTurns, 1)
	maxOutputChars := max(cfg.ContextControl.Summary.MaxOutputChars, 256)
	deterministic := deterministicSummary(previousSummary, droppedTurns, limitInput, maxOutputChars)
	if !cfg.ContextControl.Summary.Enabled || strings.TrimSpace(cfg.ContextControl.Summary.Method) != "model_written" {
		return deterministic, nil
	}
	trimmed := lastMessages(droppedTurns, limitInput)
	lines := make([]string, 0, len(trimmed))
	for _, msg := range trimmed {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		content = truncateText(content, 240)
		lines = append(lines, fmt.Sprintf("- %s: %s", strings.TrimSpace(msg.Role), content))
	}
	if len(lines) == 0 {
		return deterministic, nil
	}
	prompt := strings.TrimSpace(strings.Join([]string{
		"Existing summary:",
		summaryOrDefault(strings.TrimSpace(previousSummary), "(none)"),
		"",
		"New turns:",
		strings.Join(lines, "\n"),
		"",
		"Return a concise merged summary with:",
		"- key decisions",
		"- unresolved tasks",
		"- constraints and important tool outcomes",
		"Max 12 bullet points.",
	}, "\n"))
	maxSummaryTokens := max(maxOutputChars/4, 128)
	if maxSummaryTokens > 512 {
		maxSummaryTokens = 512
	}
	resp, _, err := e.chatWithRouting(ctx, "compactor", "session_summary", provider.ChatRequest{
		Messages: []provider.Message{
			{
				Role:    "system",
				Content: "You compress conversation state into short, factual bullets. Do not invent information.",
			},
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Model:       e.primaryModelForProcess("compactor", "session_summary"),
		MaxTokens:   maxSummaryTokens,
		Temperature: 0.1,
	})
	if err != nil {
		return deterministic, err
	}
	content := strings.TrimSpace(resp.Content)
	if content == "" {
		return deterministic, fmt.Errorf("empty summary response")
	}
	return truncateText(content, maxOutputChars), nil
}

func summaryOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func deterministicSummary(previousSummary string, droppedTurns []provider.Message, maxInputTurns, maxOutputChars int) string {
	lines := make([]string, 0, maxInputTurns+2)
	if prior := strings.TrimSpace(previousSummary); prior != "" {
		lines = append(lines, "Previous summary:", truncateText(prior, 500))
	}
	lines = append(lines, "Recent compressed history:")
	for _, msg := range lastMessages(droppedTurns, maxInputTurns) {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", strings.TrimSpace(msg.Role), truncateText(content, 180)))
	}
	return truncateText(strings.Join(lines, "\n"), maxOutputChars)
}

func lastMessages(messages []provider.Message, limit int) []provider.Message {
	if limit <= 0 || len(messages) == 0 {
		return nil
	}
	if len(messages) <= limit {
		return append([]provider.Message(nil), messages...)
	}
	return append([]provider.Message(nil), messages[len(messages)-limit:]...)
}

func isContextLengthError(err error) bool {
	if err == nil {
		return false
	}
	needle := strings.ToLower(err.Error())
	patterns := []string{
		"context length",
		"context_length_exceeded",
		"maximum context",
		"prompt is too long",
		"too many tokens",
		"context window",
	}
	for _, pattern := range patterns {
		if strings.Contains(needle, pattern) {
			return true
		}
	}
	return false
}
