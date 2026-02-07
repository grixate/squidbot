package bbolt

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/grixate/squidbot/internal/agent"
)

func TestAppendAndWindow(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.AppendTurn(ctx, agent.Turn{SessionID: "s1", Role: "user", Content: "hi"}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendTurn(ctx, agent.Turn{SessionID: "s1", Role: "assistant", Content: "hello"}); err != nil {
		t.Fatal(err)
	}

	window, err := store.Window(ctx, "s1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(window) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(window))
	}
	if window[1].Content != "hello" {
		t.Fatalf("unexpected second message: %s", window[1].Content)
	}
}
