package cortex

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memoryStore struct {
	mu sync.Mutex
	kv map[string]map[string][]byte
}

func newMemoryStore() *memoryStore {
	return &memoryStore{kv: map[string]map[string][]byte{}}
}

func (s *memoryStore) PutKV(ctx context.Context, namespace, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.kv[namespace]; !ok {
		s.kv[namespace] = map[string][]byte{}
	}
	s.kv[namespace][key] = append([]byte(nil), value...)
	return nil
}

func (s *memoryStore) GetKV(ctx context.Context, namespace, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ns, ok := s.kv[namespace]
	if !ok {
		return nil, errors.New("not found")
	}
	value, ok := ns[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return append([]byte(nil), value...), nil
}

func (s *memoryStore) ListKV(ctx context.Context, namespace, prefix string, limit int) (map[string][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ns := s.kv[namespace]
	out := make(map[string][]byte)
	for key, value := range ns {
		out[key] = append([]byte(nil), value...)
	}
	return out, nil
}

func TestServiceRegeneratePersistsBulletinAndEvent(t *testing.T) {
	store := newMemoryStore()
	now := time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
	calls := 0
	s := NewService(store, Options{
		Enabled:          true,
		BulletinMaxWords: 4,
		Now: func() time.Time {
			calls++
			return now.Add(time.Duration(calls) * time.Second)
		},
		Synthesize: func(ctx context.Context, maxWords int) (string, string, error) {
			if maxWords != 4 {
				t.Fatalf("unexpected max words: %d", maxWords)
			}
			return "one two three four five", "model-a", nil
		},
	})

	bulletin, err := s.Regenerate(context.Background(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	if bulletin != "one two three four..." {
		t.Fatalf("unexpected bulletin: %q", bulletin)
	}
	stored, err := s.Bulletin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stored != bulletin {
		t.Fatalf("stored bulletin mismatch: %q != %q", stored, bulletin)
	}
	events, err := s.Events(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Status != "succeeded" {
		t.Fatalf("expected succeeded event, got %s", events[0].Status)
	}
	if !strings.Contains(events[0].Preview, "model=model-a") {
		t.Fatalf("expected model marker in preview, got %q", events[0].Preview)
	}
}

func TestServiceRegenerateFailureFallsBackToPreviousBulletin(t *testing.T) {
	store := newMemoryStore()
	if err := store.PutKV(context.Background(), NamespaceBulletin, KeyBulletin, []byte("previous bulletin text")); err != nil {
		t.Fatal(err)
	}
	s := NewService(store, Options{
		Enabled: true,
		Now:     func() time.Time { return time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC) },
		Synthesize: func(ctx context.Context, maxWords int) (string, string, error) {
			return "", "", errors.New("synth failed")
		},
	})

	bulletin, err := s.Regenerate(context.Background(), "manual")
	if err == nil || err.Error() != "synth failed" {
		t.Fatalf("expected synth failure, got %v", err)
	}
	if bulletin != "previous bulletin text" {
		t.Fatalf("expected previous bulletin fallback, got %q", bulletin)
	}
	events, err := s.Events(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Status != "failed" {
		t.Fatalf("expected failed event, got %s", events[0].Status)
	}
	if events[0].Preview == "" {
		t.Fatal("expected fallback preview to be populated")
	}
}

func TestServiceEventsSortedAndLimited(t *testing.T) {
	store := newMemoryStore()
	now := time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
	s := NewService(store, Options{Enabled: true})
	if err := s.persistEvent(context.Background(), Event{ID: "older", Status: "succeeded", CreatedAt: now.Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := s.persistEvent(context.Background(), Event{ID: "newer", Status: "failed", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutKV(context.Background(), NamespaceEvents, "bad", []byte("{")); err != nil {
		t.Fatal(err)
	}

	events, err := s.Events(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].ID != "newer" {
		t.Fatalf("expected newest event first, got %s", events[0].ID)
	}
}

func TestServiceStartStopIdempotent(t *testing.T) {
	store := newMemoryStore()
	var calls atomic.Int32
	s := NewService(store, Options{
		Enabled:          true,
		Interval:         10 * time.Millisecond,
		BulletinMaxWords: 10,
		Now:              func() time.Time { return time.Now().UTC() },
		Synthesize: func(ctx context.Context, maxWords int) (string, string, error) {
			calls.Add(1)
			return "ok", "model", nil
		},
	})

	s.Start()
	s.Start()
	time.Sleep(35 * time.Millisecond)
	s.Stop()
	s.Stop()

	if calls.Load() == 0 {
		t.Fatal("expected background synthesize call")
	}
}
