package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureFilesystemCreatesSkillsAndDailyMemory(t *testing.T) {
	workspace := t.TempDir()
	cfg := Default()
	cfg.Agents.Defaults.Workspace = workspace

	if err := EnsureFilesystem(cfg); err != nil {
		t.Fatal(err)
	}

	checks := []string{
		filepath.Join(workspace, "AGENTS.md"),
		filepath.Join(workspace, "SOUL.md"),
		filepath.Join(workspace, "USER.md"),
		filepath.Join(workspace, "TOOLS.md"),
		filepath.Join(workspace, "HEARTBEAT.md"),
		filepath.Join(workspace, "memory", "MEMORY.md"),
		filepath.Join(workspace, "memory", "daily"),
		filepath.Join(workspace, "skills", "README.md"),
	}
	for _, path := range checks {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}
}
