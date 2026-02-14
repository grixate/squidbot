package bbolt

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/subagent"
)

func TestSubagentRunCRUDAndFiltering(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "subagent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	run1 := subagent.Run{
		ID:          "run-1",
		SessionID:   "s1",
		Task:        "one",
		Status:      subagent.StatusQueued,
		CreatedAt:   now,
		TimeoutSec:  30,
		MaxAttempts: 2,
	}
	run2 := subagent.Run{
		ID:          "run-2",
		SessionID:   "s2",
		Task:        "two",
		Status:      subagent.StatusSucceeded,
		CreatedAt:   now.Add(time.Second),
		TimeoutSec:  30,
		MaxAttempts: 2,
	}
	if err := store.PutSubagentRun(context.Background(), run1); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubagentRun(context.Background(), run2); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSubagentRun(context.Background(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Task != "one" {
		t.Fatalf("unexpected run: %+v", got)
	}
	bySession, err := store.ListSubagentRunsBySession(context.Background(), "s1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(bySession) != 1 || bySession[0].ID != "run-1" {
		t.Fatalf("unexpected session filter: %#v", bySession)
	}
	byStatus, err := store.ListSubagentRunsByStatus(context.Background(), subagent.StatusSucceeded, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(byStatus) != 1 || byStatus[0].ID != "run-2" {
		t.Fatalf("unexpected status filter: %#v", byStatus)
	}
	if err := store.AppendSubagentEvent(context.Background(), subagent.Event{RunID: "run-1", Status: subagent.StatusQueued, Message: "queued", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
}
