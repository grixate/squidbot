package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/provider"
)

var bootstrapFiles = []string{"AGENTS.md", "SOUL.md", "USER.md", "TOOLS.md"}

func buildSystemPrompt(cfg config.Config) string {
	workspace := config.WorkspacePath(cfg)
	parts := []string{
		"# squidbot",
		"",
		"You are squidbot, a practical AI assistant with tool access.",
		"",
		"## Current Time",
		time.Now().Format("2006-01-02 15:04:05 (Monday)"),
		"",
		"## Workspace",
		workspace,
		"",
	}

	for _, name := range bootstrapFiles {
		path := filepath.Join(workspace, name)
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("## %s\n\n%s", name, string(content)))
	}

	memoryPath := filepath.Join(workspace, "memory", "MEMORY.md")
	if memoryBytes, err := os.ReadFile(memoryPath); err == nil {
		parts = append(parts, "## Memory\n\n"+string(memoryBytes))
	}

	return strings.Join(parts, "\n")
}

func buildMessages(systemPrompt string, history []provider.Message, userMessage string) []provider.Message {
	messages := make([]provider.Message, 0, len(history)+2)
	messages = append(messages, provider.Message{Role: "system", Content: systemPrompt})
	messages = append(messages, history...)
	messages = append(messages, provider.Message{Role: "user", Content: userMessage})
	return messages
}
