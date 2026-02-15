package bbolt

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/federation"
)

func TestFederationRunAndIdempotencyRoundTrip(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "federation.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().UTC()
	run := federation.DelegationRun{
		ID:         "fed-run-1",
		OriginNodeID: "origin-a",
		Task:       "task",
		Status:     federation.StatusQueued,
		CreatedAt:  now,
	}
	if err := store.PutFederationRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetFederationRun(context.Background(), "fed-run-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != run.ID || got.Task != run.Task {
		t.Fatalf("unexpected run: %#v", got)
	}
	if err := store.PutFederationIdempotency(context.Background(), federation.IdempotencyRecord{
		OriginNodeID:   "origin-a",
		IdempotencyKey: "idem-1",
		RunID:          run.ID,
		CreatedAt:      now,
		ExpiresAt:      now.Add(5 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	idem, err := store.GetFederationIdempotency(context.Background(), "origin-a", "idem-1")
	if err != nil {
		t.Fatal(err)
	}
	if idem.RunID != run.ID {
		t.Fatalf("unexpected idempotency record: %#v", idem)
	}
}

func TestFederationPeerHealthRoundTrip(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "federation-health.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	health := federation.PeerHealth{
		PeerID:     "peer-a",
		Available:  true,
		QueueDepth: 1,
		MaxQueue:   8,
		ActiveRuns: 2,
		UpdatedAt:  time.Now().UTC(),
	}
	if err := store.PutFederationPeerHealth(context.Background(), health); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetFederationPeerHealth(context.Background(), "peer-a")
	if err != nil {
		t.Fatal(err)
	}
	if got.PeerID != health.PeerID || got.MaxQueue != health.MaxQueue {
		t.Fatalf("unexpected peer health: %#v", got)
	}
}

