package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/skills"
)

func TestBuildSystemPromptIncludesSkillsAndMemorySections(t *testing.T) {
	workspace := t.TempDir()
	mustWrite(t, filepath.Join(workspace, "AGENTS.md"), "# Agent\n")
	mustWrite(t, filepath.Join(workspace, "SOUL.md"), "# Soul\n")
	mustWrite(t, filepath.Join(workspace, "USER.md"), "# User\n")
	mustWrite(t, filepath.Join(workspace, "TOOLS.md"), "# Tools\n")
	mustWrite(t, filepath.Join(workspace, "HEARTBEAT.md"), "# Heartbeat\n- check inbox")
	mustWrite(t, filepath.Join(workspace, "memory", "MEMORY.md"), "The squid tracks long-term plans.")
	mustWrite(t, filepath.Join(workspace, "memory", "daily", "2026-02-09.md"), "# 2026-02-09\nDiscussed parity rollout")
	mustWrite(t, filepath.Join(workspace, "skills", "planner", "SKILL.md"), "# Planner\nCreates practical execution plans.")

	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Memory.IndexPath = filepath.Join(t.TempDir(), "memory_index.db")
	cfg.Skills.Paths = []string{"skills"}

	prompt := buildSystemPrompt(cfg, "squid")
	for _, want := range []string{
		"## Curated Memory",
		"## Retrieved Memory",
		"## Recent Daily Memory",
		"## Skill Contracts",
		"planner/SKILL.md",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected %q in prompt, got:\n%s", want, prompt)
		}
	}
}

func TestBuildSystemPromptTruncatesLargeBootstrapFiles(t *testing.T) {
	workspace := t.TempDir()
	huge := strings.Repeat("x", maxBootstrapSectionChars+500)
	mustWrite(t, filepath.Join(workspace, "AGENTS.md"), huge)
	mustWrite(t, filepath.Join(workspace, "SOUL.md"), "# Soul")
	mustWrite(t, filepath.Join(workspace, "USER.md"), "# User")
	mustWrite(t, filepath.Join(workspace, "TOOLS.md"), "# Tools")
	mustWrite(t, filepath.Join(workspace, "memory", "MEMORY.md"), "memory")

	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Memory.Enabled = false

	prompt := buildSystemPrompt(cfg, "hello")
	if strings.Contains(prompt, strings.Repeat("x", maxBootstrapSectionChars+100)) {
		t.Fatal("expected oversized AGENTS.md content to be truncated")
	}
}

func TestBuildSystemPromptWithSkillsUsesActivatedOnly(t *testing.T) {
	workspace := t.TempDir()
	mustWrite(t, filepath.Join(workspace, "AGENTS.md"), "# Agent")
	mustWrite(t, filepath.Join(workspace, "SOUL.md"), "# Soul")
	mustWrite(t, filepath.Join(workspace, "USER.md"), "# User")
	mustWrite(t, filepath.Join(workspace, "TOOLS.md"), "# Tools")
	mustWrite(t, filepath.Join(workspace, "memory", "MEMORY.md"), "memory")

	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = workspace
	activation := skills.ActivationResult{
		Activated: []skills.SkillActivation{
			{
				Skill: skills.SkillMaterialized{
					Descriptor: skills.SkillDescriptor{Name: "Planner", ID: "planner", Path: filepath.Join(workspace, "skills", "planner", "SKILL.md")},
					Body:       "Use staged planning.",
				},
				Score:  1000,
				Reason: "explicit",
			},
		},
		Diagnostics: skills.ActivationDiagnostics{Matched: 3, Activated: 1, Skipped: 2},
	}
	prompt := buildSystemPromptWithSkills(cfg, "plan", &activation)
	if !strings.Contains(prompt, "Planner [planner]") {
		t.Fatalf("expected activated skill in prompt, got:\n%s", prompt)
	}
	if strings.Contains(prompt, "No summary provided") {
		t.Fatalf("did not expect full discovery list when activation exists, got:\n%s", prompt)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
