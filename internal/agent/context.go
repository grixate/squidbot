package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/memory"
	"github.com/grixate/squidbot/internal/provider"
	"github.com/grixate/squidbot/internal/skills"
)

var bootstrapFiles = []string{"AGENTS.md", "SOUL.md", "USER.md", "TOOLS.md"}

const (
	maxBootstrapSectionChars = 5000
	maxMemorySnippetChars    = 360
)

func buildSystemPrompt(cfg config.Config, userMessage string) string {
	return buildSystemPromptWithSkillsAndOptions(cfg, userMessage, nil, defaultPromptBuildOptions(cfg))
}

func buildSystemPromptWithSkills(cfg config.Config, userMessage string, activation *skills.ActivationResult) string {
	return buildSystemPromptWithSkillsAndOptions(cfg, userMessage, activation, defaultPromptBuildOptions(cfg))
}

type PromptBuildOptions struct {
	BootstrapMaxChars      int
	MemorySnippetMaxChars  int
	IncludeCuratedMemory   bool
	IncludeRetrievedMemory bool
	IncludeRecentDaily     bool
	IncludeSkills          bool
	TopK                   int
	RecentDailyLimit       int
	SkillPromptMaxChars    int
	SessionSummary         string
	Bulletin               string
}

func defaultPromptBuildOptions(cfg config.Config) PromptBuildOptions {
	return PromptBuildOptions{
		BootstrapMaxChars:      maxBootstrapSectionChars,
		MemorySnippetMaxChars:  maxMemorySnippetChars,
		IncludeCuratedMemory:   true,
		IncludeRetrievedMemory: true,
		IncludeRecentDaily:     true,
		IncludeSkills:          true,
		TopK:                   cfg.Memory.TopK,
		RecentDailyLimit:       minInt(4, cfg.Memory.TopK),
		SkillPromptMaxChars:    cfg.Skills.PromptMaxChars,
	}
}

func normalizePromptBuildOptions(cfg config.Config, options PromptBuildOptions) PromptBuildOptions {
	if options.BootstrapMaxChars <= 0 {
		options.BootstrapMaxChars = maxBootstrapSectionChars
	}
	if options.MemorySnippetMaxChars <= 0 {
		options.MemorySnippetMaxChars = maxMemorySnippetChars
	}
	if options.TopK <= 0 {
		options.TopK = cfg.Memory.TopK
	}
	if options.TopK <= 0 {
		options.TopK = 8
	}
	if options.RecentDailyLimit <= 0 {
		options.RecentDailyLimit = minInt(4, options.TopK)
	}
	if options.RecentDailyLimit <= 0 {
		options.RecentDailyLimit = 4
	}
	if options.SkillPromptMaxChars <= 0 {
		options.SkillPromptMaxChars = cfg.Skills.PromptMaxChars
	}
	if options.SkillPromptMaxChars <= 0 {
		options.SkillPromptMaxChars = 12000
	}
	return options
}

func buildSystemPromptWithSkillsAndOptions(cfg config.Config, userMessage string, activation *skills.ActivationResult, options PromptBuildOptions) string {
	options = normalizePromptBuildOptions(cfg, options)
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
		parts = append(parts, fmt.Sprintf("## %s\n\n%s", name, truncateText(string(content), options.BootstrapMaxChars)))
	}

	if options.IncludeCuratedMemory {
		memoryPath := filepath.Join(workspace, "memory", "MEMORY.md")
		if memoryBytes, err := os.ReadFile(memoryPath); err == nil {
			parts = append(parts, "## Curated Memory\n\n"+truncateText(string(memoryBytes), options.BootstrapMaxChars))
		}
	}

	memoryManager := memory.NewManager(cfg)
	if memoryManager.Enabled() {
		ctx := context.Background()
		_ = memoryManager.Sync(ctx)

		if options.IncludeRetrievedMemory {
			retrieved, err := memoryManager.Search(ctx, userMessage, options.TopK)
			if err == nil && len(retrieved) > 0 {
				lines := make([]string, 0, len(retrieved))
				for _, chunk := range retrieved {
					lines = append(lines, fmt.Sprintf("- %s: %s", shortPath(workspace, chunk.Path), truncateText(chunk.Content, options.MemorySnippetMaxChars)))
				}
				parts = append(parts, "## Retrieved Memory\n\n"+strings.Join(lines, "\n"))
			}
		}

		if options.IncludeRecentDaily {
			recentDaily, err := memoryManager.RecentDaily(ctx, options.RecentDailyLimit)
			if err == nil && len(recentDaily) > 0 {
				lines := make([]string, 0, len(recentDaily))
				for _, chunk := range recentDaily {
					lines = append(lines, fmt.Sprintf("- %s: %s", shortPath(workspace, chunk.Path), truncateText(chunk.Content, options.MemorySnippetMaxChars)))
				}
				parts = append(parts, "## Recent Daily Memory\n\n"+strings.Join(lines, "\n"))
			}
		}
	}

	if summary := strings.TrimSpace(options.SessionSummary); summary != "" {
		parts = append(parts, "## Session Summary\n\n"+truncateText(summary, options.BootstrapMaxChars))
	}
	if bulletin := strings.TrimSpace(options.Bulletin); bulletin != "" {
		parts = append(parts, "## Cortex Bulletin\n\n"+truncateText(bulletin, options.BootstrapMaxChars))
	}

	if options.IncludeSkills {
		skillsCfg := cfg
		skillsCfg.Skills.PromptMaxChars = options.SkillPromptMaxChars
		if section := renderSkillContractsSection(skillsCfg, workspace, activation); strings.TrimSpace(section) != "" {
			parts = append(parts, section)
		}
	}

	return strings.Join(parts, "\n")
}

func renderSkillContractsSection(cfg config.Config, workspace string, activation *skills.ActivationResult) string {
	if activation == nil {
		discovery := skills.Discover(cfg)
		if len(discovery.Skills) == 0 {
			return ""
		}
		lines := make([]string, 0, len(discovery.Skills))
		for _, skill := range discovery.Skills {
			lines = append(lines, fmt.Sprintf("- %s (%s): %s", skill.Name, shortPath(workspace, skill.Path), truncateText(skill.Summary, 240)))
		}
		return "## Skill Contracts\n\n" + strings.Join(lines, "\n")
	}
	if len(activation.Activated) == 0 {
		return ""
	}
	lines := make([]string, 0, len(activation.Activated)+1)
	lines = append(lines, fmt.Sprintf("Matched: %d, Activated: %d, Skipped: %d", activation.Diagnostics.Matched, activation.Diagnostics.Activated, activation.Diagnostics.Skipped))
	maxChars := cfg.Skills.PromptMaxChars
	if maxChars <= 0 {
		maxChars = 12000
	}
	for _, activated := range activation.Activated {
		desc := activated.Skill.Descriptor
		referenceLine := "none"
		if len(activated.Skill.ResolvedReferences) > 0 {
			shortRefs := make([]string, 0, len(activated.Skill.ResolvedReferences))
			for _, ref := range activated.Skill.ResolvedReferences {
				shortRefs = append(shortRefs, shortPath(workspace, ref))
			}
			referenceLine = strings.Join(shortRefs, ", ")
		}
		body := truncateText(activated.Skill.Body, maxInt(cfg.Skills.SkillMaxChars, 1))
		lines = append(lines,
			fmt.Sprintf("- %s [%s] (%s) reason=%s score=%d refs=%s\n%s",
				desc.Name,
				desc.ID,
				shortPath(workspace, desc.Path),
				activated.Reason,
				activated.Score,
				referenceLine,
				body,
			),
		)
		joined := strings.Join(lines, "\n\n")
		if len(joined) > maxChars {
			lines = lines[:len(lines)-1]
			break
		}
	}
	return "## Skill Contracts\n\n" + strings.Join(lines, "\n\n")
}

func buildMessages(systemPrompt string, history []provider.Message, userMessage string) []provider.Message {
	messages := make([]provider.Message, 0, len(history)+2)
	messages = append(messages, provider.Message{Role: "system", Content: systemPrompt})
	messages = append(messages, history...)
	messages = append(messages, provider.Message{Role: "user", Content: userMessage})
	return messages
}

func truncateText(content string, maxChars int) string {
	content = strings.TrimSpace(content)
	if maxChars <= 0 || len(content) <= maxChars {
		return content
	}
	if maxChars <= 3 {
		return content[:maxChars]
	}
	return content[:maxChars-3] + "..."
}

func shortPath(workspace, fullPath string) string {
	if rel, err := filepath.Rel(workspace, fullPath); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(fullPath)
}

func minInt(a, b int) int {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}

func maxInt(v, floor int) int {
	if v < floor {
		return floor
	}
	return v
}
