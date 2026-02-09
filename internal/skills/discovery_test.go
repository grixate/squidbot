package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grixate/squidbot/internal/config"
)

func TestDiscoverFindsAndSortsSkills(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "skills", "zeta"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "skills", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "skills", "zeta", "SKILL.md"), []byte("# Zeta\nDo zeta things"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "skills", "alpha", "SKILL.md"), []byte("# Alpha\nDo alpha things"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Skills.Paths = []string{"skills"}

	result := Discover(cfg)
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings, got: %#v", result.Warnings)
	}
	if len(result.Skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(result.Skills))
	}
	if result.Skills[0].Name != "Alpha" || result.Skills[1].Name != "Zeta" {
		t.Fatalf("skills not sorted deterministically: %#v", result.Skills)
	}
}

func TestDiscoverSoftFailsForMissingRoots(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Skills.Paths = []string{filepath.Join(workspace, "does-not-exist")}

	result := Discover(cfg)
	if len(result.Skills) != 0 {
		t.Fatalf("expected no skills, got %d", len(result.Skills))
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("expected missing roots to be ignored, got warnings: %#v", result.Warnings)
	}
}
