package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/cortex"
	"github.com/grixate/squidbot/internal/provider"
)

func (e *Engine) generateCortexBulletin(ctx context.Context, maxWords int) (string, string, error) {
	cfg := e.currentConfig()
	workspace := config.WorkspacePath(cfg)
	curated := ""
	memoryPath := filepath.Join(workspace, "memory", "MEMORY.md")
	if raw, err := os.ReadFile(memoryPath); err == nil {
		curated = truncateText(string(raw), 1800)
	}
	dailyLines := []string{}
	if e.memory != nil && e.memory.Enabled() {
		recent, err := e.memory.RecentDaily(ctx, 6)
		if err == nil {
			for _, chunk := range recent {
				dailyLines = append(dailyLines, fmt.Sprintf("- %s", truncateText(strings.TrimSpace(chunk.Content), 220)))
			}
		}
	}
	if len(dailyLines) == 0 {
		dailyLines = append(dailyLines, "- none")
	}
	if strings.TrimSpace(curated) == "" {
		curated = "(empty)"
	}
	req := provider.ChatRequest{
		Messages: []provider.Message{
			{
				Role:    "system",
				Content: "You generate a short operational bulletin for the next channel turn. Keep it factual and compact.",
			},
			{
				Role: "user",
				Content: strings.Join([]string{
					"Create an ambient bulletin with identity, recent outcomes, important decisions, and open items.",
					fmt.Sprintf("Max words: %d", max(maxWords, 60)),
					"",
					"Curated memory:",
					curated,
					"",
					"Recent daily memory:",
					strings.Join(dailyLines, "\n"),
				}, "\n"),
			},
		},
		Model:       e.primaryModelForProcess("cortex", "bulletin"),
		MaxTokens:   max(maxWords*2, 160),
		Temperature: 0.15,
	}
	resp, model, err := e.chatWithRouting(ctx, "cortex", "bulletin", req)
	if err != nil {
		return "", model, err
	}
	return strings.TrimSpace(resp.Content), model, nil
}

func (e *Engine) currentCortexBulletin(ctx context.Context) string {
	if e == nil || e.cortex == nil {
		return ""
	}
	bulletin, err := e.cortex.Bulletin(ctx)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(bulletin)
}

func (e *Engine) listCortexEvents(ctx context.Context, limit int) ([]cortex.Event, error) {
	if e == nil || e.cortex == nil {
		return nil, nil
	}
	return e.cortex.Events(ctx, limit)
}
