package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/branch"
	"github.com/grixate/squidbot/internal/compaction"
	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/provider"
	"github.com/grixate/squidbot/internal/tools"
)

func (e *Engine) spawnBranch(ctx context.Context, req tools.BranchSpawnRequest) (branch.BranchRun, error) {
	if e.branches == nil {
		return branch.BranchRun{}, fmt.Errorf("branch manager is not configured")
	}
	run, err := e.branches.Enqueue(ctx, branch.Request{
		SessionID:   req.SessionID,
		Description: req.Description,
		Prompt:      req.Prompt,
	})
	if err != nil {
		return branch.BranchRun{}, err
	}
	if req.Wait {
		timeout := 60 * time.Second
		if req.TimeoutSec > 0 {
			timeout = time.Duration(req.TimeoutSec) * time.Second
		}
		return e.branches.Wait(ctx, run.ID, timeout)
	}
	return run, nil
}

func (e *Engine) branchStatus(ctx context.Context, req tools.BranchStatusRequest) (branch.BranchRun, error) {
	if e.branches == nil {
		return branch.BranchRun{}, fmt.Errorf("branch manager is not configured")
	}
	run, err := e.branches.Status(ctx, req.BranchID)
	if err != nil {
		return branch.BranchRun{}, err
	}
	if err := ensureBranchRunAccess(req.SessionID, run); err != nil {
		return branch.BranchRun{}, err
	}
	return run, nil
}

func (e *Engine) branchWait(ctx context.Context, req tools.BranchWaitRequest) (branch.BranchRun, error) {
	if e.branches == nil {
		return branch.BranchRun{}, fmt.Errorf("branch manager is not configured")
	}
	timeout := time.Duration(req.TimeoutSec) * time.Second
	run, err := e.branches.Wait(ctx, req.BranchID, timeout)
	if err != nil {
		return run, err
	}
	if err := ensureBranchRunAccess(req.SessionID, run); err != nil {
		return branch.BranchRun{}, err
	}
	return run, nil
}

func ensureBranchRunAccess(sessionID string, run branch.BranchRun) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	if strings.TrimSpace(run.SessionID) == sessionID {
		return nil
	}
	return fmt.Errorf("branch run %s does not belong to session %s", run.ID, sessionID)
}

func (e *Engine) runBranch(ctx context.Context, run branch.BranchRun) (string, string, error) {
	cfg := e.currentConfig()
	messages := []provider.Message{
		{
			Role: "system",
			Content: strings.TrimSpace(strings.Join([]string{
				"You are a branch process focused on reasoning quality.",
				"Prefer memory tools and concise synthesis over execution.",
				"Do not invoke filesystem or shell tools.",
			}, " ")),
		},
	}
	if strings.TrimSpace(run.Description) != "" {
		messages = append(messages, provider.Message{
			Role:    "user",
			Content: "Branch description: " + strings.TrimSpace(run.Description),
		})
	}
	messages = append(messages, provider.Message{
		Role:    "user",
		Content: strings.TrimSpace(run.Prompt),
	})
	registry := tools.NewRegistry()
	registry.Register(tools.NewMemoryRecallTool(e.branchMemoryRecall))
	registry.Register(tools.NewMemorySaveTool(e.branchMemorySave))
	registry.Register(tools.NewMemoryDeleteTool(e.branchMemoryDelete))
	registry.Register(tools.NewChannelRecallTool(func(ctx context.Context, limit int) ([]string, error) {
		return e.branchChannelRecall(ctx, run.SessionID, limit)
	}))

	maxHops := max(cfg.Agents.Defaults.MaxToolIterations, 6)
	if maxHops > 10 {
		maxHops = 10
	}
	final := ""
	usedModel := ""
	for i := 0; i < maxHops; i++ {
		resp, model, err := e.chatWithRouting(ctx, "branch", strings.ToLower(strings.TrimSpace(run.Description)), provider.ChatRequest{
			Messages:    messages,
			Tools:       registry.Definitions(),
			Model:       e.primaryModelForProcess("branch", strings.ToLower(strings.TrimSpace(run.Description))),
			MaxTokens:   max(cfg.Agents.Defaults.MaxTokens/2, 256),
			Temperature: 0.2,
		})
		if err != nil {
			return "", usedModel, err
		}
		if strings.TrimSpace(model) != "" {
			usedModel = model
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
		final = strings.TrimSpace(resp.Content)
		if final == "" {
			final = "Branch completed with no explicit conclusion."
		}
		break
	}
	if strings.TrimSpace(final) == "" {
		final = "Branch completed with no explicit conclusion."
	}
	return final, usedModel, nil
}

func (e *Engine) branchMemoryRecall(ctx context.Context, query string, limit int) ([]string, error) {
	if e.memory == nil || !e.memory.Enabled() {
		return nil, nil
	}
	chunks, err := e.memory.Search(ctx, strings.TrimSpace(query), limit)
	if err != nil {
		return nil, err
	}
	workspace := config.WorkspacePath(e.currentConfig())
	out := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		out = append(out, fmt.Sprintf("%s: %s", shortPath(workspace, chunk.Path), truncateText(chunk.Content, 220)))
	}
	return out, nil
}

func (e *Engine) branchMemorySave(ctx context.Context, entry string) (string, error) {
	cfg := e.currentConfig()
	workspace := config.WorkspacePath(cfg)
	memoryPath := filepath.Join(workspace, "memory", "MEMORY.md")
	if err := os.MkdirAll(filepath.Dir(memoryPath), 0o755); err != nil {
		return "", err
	}
	if _, err := os.Stat(memoryPath); os.IsNotExist(err) {
		if writeErr := os.WriteFile(memoryPath, []byte("# MEMORY\n\n"), 0o644); writeErr != nil {
			return "", writeErr
		}
	}
	f, err := os.OpenFile(memoryPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	line := "- " + time.Now().UTC().Format(time.RFC3339) + " " + strings.TrimSpace(entry) + "\n"
	if _, err := f.WriteString(line); err != nil {
		return "", err
	}
	if e.memory != nil && e.memory.Enabled() {
		_ = e.memory.Sync(ctx)
	}
	return memoryPath, nil
}

func (e *Engine) branchMemoryDelete(ctx context.Context, query string) (int, error) {
	cfg := e.currentConfig()
	workspace := config.WorkspacePath(cfg)
	memoryPath := filepath.Join(workspace, "memory", "MEMORY.md")
	raw, err := os.ReadFile(memoryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	target := strings.ToLower(strings.TrimSpace(query))
	if target == "" {
		return 0, nil
	}
	lines := strings.Split(string(raw), "\n")
	kept := make([]string, 0, len(lines))
	removed := 0
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), target) {
			removed++
			continue
		}
		kept = append(kept, line)
	}
	if removed == 0 {
		return 0, nil
	}
	if err := os.WriteFile(memoryPath, []byte(strings.Join(kept, "\n")), 0o644); err != nil {
		return 0, err
	}
	if e.memory != nil && e.memory.Enabled() {
		_ = e.memory.Sync(ctx)
	}
	return removed, nil
}

func (e *Engine) branchChannelRecall(ctx context.Context, sessionID string, limit int) ([]string, error) {
	history, err := e.store.Window(ctx, strings.TrimSpace(sessionID), limit)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(history))
	for _, msg := range history {
		out = append(out, fmt.Sprintf("%s: %s", strings.TrimSpace(msg.Role), truncateText(strings.TrimSpace(msg.Content), 180)))
	}
	return out, nil
}

func (e *Engine) summarizeCompactionTurns(ctx context.Context, sessionID string, removed []compaction.Turn) (string, error) {
	if len(removed) == 0 {
		return "", nil
	}
	lines := make([]string, 0, minInt(len(removed), 16))
	start := 0
	if len(removed) > 16 {
		start = len(removed) - 16
	}
	for _, turn := range removed[start:] {
		lines = append(lines, fmt.Sprintf("- %s: %s", strings.TrimSpace(turn.Role), truncateText(turn.Content, 180)))
	}
	prompt := strings.Join([]string{
		"Summarize the removed conversation turns for future context.",
		"Return concise bullets with key decisions, constraints, and pending items.",
		"Transcript:",
		strings.Join(lines, "\n"),
	}, "\n\n")
	if e.branches == nil {
		return prompt, nil
	}
	run, err := e.branches.Enqueue(ctx, branch.Request{
		SessionID:   sessionID,
		Description: "compaction",
		Prompt:      prompt,
	})
	if err != nil {
		return "", err
	}
	waited, err := e.branches.Wait(ctx, run.ID, 45*time.Second)
	if err != nil {
		return waited.Conclusion, err
	}
	return strings.TrimSpace(waited.Conclusion), nil
}
