package subagent_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	storepkg "github.com/grixate/squidbot/internal/storage/bbolt"
	"github.com/grixate/squidbot/internal/subagent"
)

func TestManagerRecoveryRequeuesRunningAndQueuedRuns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "recovery.db")
	store, err := storepkg.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().UTC()
	running := subagent.Run{
		ID:          "run-recover-running",
		SessionID:   "s1",
		Task:        "running-task",
		Status:      subagent.StatusRunning,
		CreatedAt:   now,
		StartedAt:   &now,
		TimeoutSec:  10,
		MaxAttempts: 1,
	}
	queued := subagent.Run{
		ID:          "run-recover-queued",
		SessionID:   "s1",
		Task:        "queued-task",
		Status:      subagent.StatusQueued,
		CreatedAt:   now,
		TimeoutSec:  10,
		MaxAttempts: 1,
	}
	if err := store.PutSubagentRun(context.Background(), running); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubagentRun(context.Background(), queued); err != nil {
		t.Fatal(err)
	}

	mgr := subagent.NewManager(subagent.Options{
		Enabled:        true,
		MaxConcurrent:  2,
		MaxQueue:       8,
		DefaultTimeout: 2 * time.Second,
		MaxAttempts:    1,
		NextID:         func() string { return "evt-recovery" },
	}, store, func(ctx context.Context, run subagent.Run) (subagent.Result, error) {
		return subagent.Result{Summary: "ok", Output: "ok"}, nil
	}, nil, nil)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop()

	runs, err := mgr.Wait(context.Background(), []string{"run-recover-running", "run-recover-queued"}, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
	for _, run := range runs {
		if run.Status != subagent.StatusSucceeded {
			t.Fatalf("expected succeeded, got %s for %s", run.Status, run.ID)
		}
	}
}
