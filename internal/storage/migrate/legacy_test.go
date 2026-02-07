package migrate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/grixate/squidbot/internal/config"
	storepkg "github.com/grixate/squidbot/internal/storage/bbolt"
)

func TestImportLegacy(t *testing.T) {
	legacy := t.TempDir()
	if err := os.MkdirAll(filepath.Join(legacy, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(legacy, "cron"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(legacy, "workspace", "memory"), 0o755); err != nil {
		t.Fatal(err)
	}

	sessionData := `{"_type":"metadata","metadata":{"source":"legacy"}}
{"role":"user","content":"hello","timestamp":"2026-02-07T10:00:00"}
{"role":"assistant","content":"hi","timestamp":"2026-02-07T10:00:01"}
`
	if err := os.WriteFile(filepath.Join(legacy, "sessions", "cli_default.jsonl"), []byte(sessionData), 0o644); err != nil {
		t.Fatal(err)
	}

	cronData := `{"version":1,"jobs":[{"id":"abc","name":"daily","enabled":true,"schedule":{"kind":"every","everyMs":60000},"payload":{"message":"ping","deliver":false},"state":{},"createdAtMs":1730000000000,"updatedAtMs":1730000000000}]}`
	if err := os.WriteFile(filepath.Join(legacy, "cron", "jobs.json"), []byte(cronData), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "workspace", "AGENTS.md"), []byte("legacy agents"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "workspace", "memory", "MEMORY.md"), []byte("legacy memory"), 0o644); err != nil {
		t.Fatal(err)
	}

	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(filepath.Join(workspace, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = workspace

	dbPath := filepath.Join(t.TempDir(), "squidbot.db")
	store, err := storepkg.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	report, err := ImportLegacy(context.Background(), legacy, filepath.Join(t.TempDir(), "config.json"), cfg, store, false)
	if err != nil {
		t.Fatal(err)
	}

	if report.SessionsImported != 1 {
		t.Fatalf("expected 1 session imported, got %d", report.SessionsImported)
	}
	if report.TurnsImported != 2 {
		t.Fatalf("expected 2 turns imported, got %d", report.TurnsImported)
	}
	if report.JobsImported != 1 {
		t.Fatalf("expected 1 job imported, got %d", report.JobsImported)
	}
	if report.FilesCopied == 0 {
		t.Fatalf("expected copied files > 0")
	}

	window, err := store.Window(context.Background(), "cli:default", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(window) != 2 {
		t.Fatalf("expected 2 window messages, got %d", len(window))
	}
}
