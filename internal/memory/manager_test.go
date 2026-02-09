package memory

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/config"
)

func TestSyncIdempotentAndStaleCleanup(t *testing.T) {
	workspace := t.TempDir()
	memoryDir := filepath.Join(workspace, "memory")
	dailyDir := filepath.Join(memoryDir, "daily")
	if err := os.MkdirAll(dailyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	curatedPath := filepath.Join(memoryDir, "MEMORY.md")
	if err := os.WriteFile(curatedPath, []byte("# Memory\nalpha\n\nbeta"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dailyDir, "2026-02-09.md"), []byte("# 2026-02-09\nalpha daily"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Memory.IndexPath = filepath.Join(t.TempDir(), "memory_index.db")
	cfg.Memory.EmbeddingsProvider = "none"

	mgr := NewManager(cfg)
	if err := mgr.Sync(context.Background()); err != nil {
		t.Fatalf("first sync failed: %v", err)
	}
	firstCount := countChunks(t, cfg.Memory.IndexPath)
	if firstCount == 0 {
		t.Fatal("expected indexed chunks")
	}
	if err := mgr.Sync(context.Background()); err != nil {
		t.Fatalf("second sync failed: %v", err)
	}
	secondCount := countChunks(t, cfg.Memory.IndexPath)
	if firstCount != secondCount {
		t.Fatalf("sync should be idempotent, got %d then %d", firstCount, secondCount)
	}

	if err := os.WriteFile(curatedPath, []byte("# Memory\nbeta only"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dailyDir, "2026-02-09.md")); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Sync(context.Background()); err != nil {
		t.Fatalf("sync after update failed: %v", err)
	}
	results, err := mgr.Search(context.Background(), "alpha", 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected stale alpha chunks to be removed, got %d", len(results))
	}
}

func TestSearchFallbackWhenEmbeddingsDisabled(t *testing.T) {
	workspace := t.TempDir()
	memoryDir := filepath.Join(workspace, "memory")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, "MEMORY.md"), []byte("The squid loves practical plans."), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Memory.IndexPath = filepath.Join(t.TempDir(), "memory_index.db")
	cfg.Memory.EmbeddingsProvider = "none"
	mgr := NewManager(cfg)
	if err := mgr.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	results, err := mgr.Search(context.Background(), "practical", 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected lexical search results with embeddings disabled")
	}
}

func TestAppendDailyLogFormat(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "memory", "MEMORY.md"), []byte("# Memory"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Memory.IndexPath = filepath.Join(t.TempDir(), "memory_index.db")

	mgr := NewManager(cfg)
	entryTime := time.Date(2026, 2, 9, 15, 30, 0, 0, time.UTC)
	if err := mgr.AppendDailyLog(context.Background(), DailyEntry{
		Time:      entryTime,
		Source:    "conversation",
		SessionID: "cli:default",
		Intent:    "User asked for parity details",
		Outcome:   "Implemented configuration changes",
		FollowUp:  true,
	}); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(workspace, "memory", "daily", "2026-02-09.md")
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(bytes)
	for _, want := range []string{
		"# 2026-02-09",
		"## 15:30:00Z [conversation]",
		"- Session: cli:default",
		"- Intent: User asked for parity details",
		"- Outcome: Implemented configuration changes",
		"- Follow-up: yes",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %q in daily log, got:\n%s", want, content)
		}
	}
}

func countChunks(t *testing.T, indexPath string) int {
	t.Helper()
	db, err := sql.Open("sqlite", indexPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM chunks`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
