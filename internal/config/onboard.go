package config

import (
	"os"
	"path/filepath"
)

var workspaceTemplates = map[string]string{
	"AGENTS.md": `# Agent Instructions

You are squidbot, a helpful AI assistant. Be concise, accurate, and practical.

## Guidelines

- Explain intent before taking actions.
- Ask for clarification when requests are ambiguous.
- Prefer safe, reversible operations.
- Record durable user preferences in memory/MEMORY.md.
`,
	"SOUL.md": `# Soul

I am squidbot.

## Personality

- Calm and direct
- Action-oriented
- Honest about uncertainty
`,
	"USER.md": `# User

## Preferences

- Communication style:
- Timezone:
- Language:
`,
	"TOOLS.md": `# Tools

## Available

- read_file(path)
- write_file(path, content)
- edit_file(path, old_text, new_text)
- list_dir(path)
- exec(command, working_dir?)
- web_search(query, count?)
- web_fetch(url, extractMode?, maxChars?)
- message(content, channel?, chat_id?)
- spawn(task, label?, context_mode?, attachments?, timeout_sec?, max_attempts?, wait?)
- subagent_wait(run_ids, timeout_sec?)
- subagent_status(run_id)
- subagent_result(run_id)
- subagent_cancel(run_id)
`,
	"HEARTBEAT.md": `# Heartbeat Tasks

This file is checked periodically by squidbot.

## Active Tasks

<!-- Add periodic tasks below -->
`,
	"skills/README.md": `# Skills

Place custom skills under this directory.

Each skill should include a SKILL.md contract file, for example:

skills/
  my-skill/
    SKILL.md
`,
}

const memoryTemplate = `# Long-term Memory

## User Information

## Preferences

## Important Notes
`

func EnsureFilesystem(cfg Config) error {
	home := HomeDir()
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(DataRoot(), 0o755); err != nil {
		return err
	}
	workspace := WorkspacePath(cfg)
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return err
	}
	memoryDir := filepath.Join(workspace, "memory")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		return err
	}
	memoryDailyDir := filepath.Join(memoryDir, "daily")
	if err := os.MkdirAll(memoryDailyDir, 0o755); err != nil {
		return err
	}

	for name, content := range workspaceTemplates {
		path := filepath.Join(workspace, name)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}

	memoryPath := filepath.Join(memoryDir, "MEMORY.md")
	if _, err := os.Stat(memoryPath); os.IsNotExist(err) {
		if err := os.WriteFile(memoryPath, []byte(memoryTemplate), 0o644); err != nil {
			return err
		}
	}
	return nil
}
