package bbolt

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/federation"
)

func TestFederationRunStoreRoundTrip(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "federation.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := federation.DelegationRun{
		ID:        "run-1",
		Task:      "task",
		Status:    federation.StatusQueued,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.PutFederationRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetFederationRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != run.ID {
		t.Fatalf("unexpected run: %#v", got)
	}
}

