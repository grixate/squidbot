package branch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
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

func mustStoreRun(t *testing.T, store *memoryStore, run BranchRun) {
	t.Helper()
	raw, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutKV(context.Background(), kvNamespaceRuns, run.ID, raw); err != nil {
		t.Fatal(err)
	}
}

func loadStoredRun(t *testing.T, store *memoryStore, id string) BranchRun {
	t.Helper()
	raw, err := store.GetKV(context.Background(), kvNamespaceRuns, id)
	if err != nil {
		t.Fatal(err)
	}
	var run BranchRun
	if err := json.Unmarshal(raw, &run); err != nil {
		t.Fatal(err)
	}
	return run
}

func TestManagerEnqueueValidation(t *testing.T) {
	store := newMemoryStore()
	m := NewManager(Options{Enabled: false}, store, func(ctx context.Context, run BranchRun) (string, string, error) {
		return "", "", nil
	})
	if _, err := m.Enqueue(context.Background(), Request{Prompt: "hello"}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}

	m = NewManager(Options{Enabled: true}, store, func(ctx context.Context, run BranchRun) (string, string, error) {
		return "", "", nil
	})
	if _, err := m.Enqueue(context.Background(), Request{Prompt: "   "}); err == nil || err.Error() != "prompt is required" {
		t.Fatalf("expected prompt required, got %v", err)
	}
}

func TestManagerQueueFullPersistsFailure(t *testing.T) {
	store := newMemoryStore()
	nextID := 0
	m := NewManager(Options{
		Enabled:       true,
		MaxConcurrent: 1,
		MaxQueue:      1,
		NextID: func() string {
			nextID++
			return fmt.Sprintf("run-%d", nextID)
		},
	}, store, func(ctx context.Context, run BranchRun) (string, string, error) {
		return "ok", "test", nil
	})

	if _, err := m.Enqueue(context.Background(), Request{Prompt: "one"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Enqueue(context.Background(), Request{Prompt: "two"}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}

	run := loadStoredRun(t, store, "run-2")
	if run.Status != StatusFailed {
		t.Fatalf("expected failed status, got %s", run.Status)
	}
	if run.Error != ErrQueueFull.Error() {
		t.Fatalf("expected queue full error, got %q", run.Error)
	}
	if run.CompletedAt.IsZero() {
		t.Fatal("expected completed timestamp")
	}
}

func TestManagerRunSuccessAndFailure(t *testing.T) {
	store := newMemoryStore()
	m := NewManager(Options{
		Enabled:        true,
		MaxConcurrent:  1,
		MaxQueue:       8,
		DefaultTimeout: time.Second,
		NextID: func() string {
			return fmt.Sprintf("run-%d", time.Now().UnixNano())
		},
	}, store, func(ctx context.Context, run BranchRun) (string, string, error) {
		if run.Prompt == "fail" {
			return "", "worker-model", errors.New("boom")
		}
		return "done", "worker-model", nil
	})
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	successRun, err := m.Enqueue(context.Background(), Request{ID: "success", Prompt: "ok", SessionID: "session-a"})
	if err != nil {
		t.Fatal(err)
	}
	failedRun, err := m.Enqueue(context.Background(), Request{ID: "failed", Prompt: "fail", SessionID: "session-a"})
	if err != nil {
		t.Fatal(err)
	}

	success, err := m.Wait(context.Background(), successRun.ID, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if success.Status != StatusSucceeded || success.Conclusion != "done" || success.Model != "worker-model" {
		t.Fatalf("unexpected success run: %+v", success)
	}
	if success.StartedAt.IsZero() || success.CompletedAt.IsZero() {
		t.Fatalf("expected run timestamps: %+v", success)
	}

	failed, err := m.Wait(context.Background(), failedRun.ID, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != StatusFailed || failed.Error != "boom" || failed.Model != "worker-model" {
		t.Fatalf("unexpected failed run: %+v", failed)
	}
}

func TestManagerWaitTimeout(t *testing.T) {
	store := newMemoryStore()
	started := make(chan struct{}, 1)
	block := make(chan struct{})
	m := NewManager(Options{
		Enabled:        true,
		MaxConcurrent:  1,
		MaxQueue:       8,
		DefaultTimeout: 2 * time.Second,
		NextID:         func() string { return "slow-run" },
	}, store, func(ctx context.Context, run BranchRun) (string, string, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-block:
			return "done", "model", nil
		case <-ctx.Done():
			return "", "", ctx.Err()
		}
	})
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	if _, err := m.Enqueue(context.Background(), Request{Prompt: "slow"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("run did not start")
	}

	run, err := m.Wait(context.Background(), "slow-run", 50*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	if run.Status.Terminal() {
		t.Fatalf("expected non-terminal run, got %s", run.Status)
	}
	close(block)
}

func TestManagerListFiltersSortsAndLimits(t *testing.T) {
	store := newMemoryStore()
	now := time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
	mustStoreRun(t, store, BranchRun{ID: "older", SessionID: "s1", Prompt: "a", Status: StatusSucceeded, CreatedAt: now.Add(-2 * time.Minute)})
	mustStoreRun(t, store, BranchRun{ID: "newer", SessionID: "s1", Prompt: "b", Status: StatusSucceeded, CreatedAt: now})
	mustStoreRun(t, store, BranchRun{ID: "other-session", SessionID: "s2", Prompt: "c", Status: StatusSucceeded, CreatedAt: now.Add(-time.Minute)})
	if err := store.PutKV(context.Background(), kvNamespaceRuns, "bad-json", []byte("{")); err != nil {
		t.Fatal(err)
	}

	m := NewManager(Options{Enabled: true}, store, func(ctx context.Context, run BranchRun) (string, string, error) {
		return "", "", nil
	})

	runs, err := m.List(context.Background(), "s1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].ID != "newer" {
		t.Fatalf("expected newest run first, got %s", runs[0].ID)
	}

	all, err := m.List(context.Background(), "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 valid runs, got %d", len(all))
	}
}
