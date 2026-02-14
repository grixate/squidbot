package subagent

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memoryStore struct {
	mu     sync.Mutex
	runs   map[string]Run
	events []Event
	kv     map[string]map[string][]byte
}

func newMemoryStore() *memoryStore {
	return &memoryStore{runs: map[string]Run{}, events: []Event{}, kv: map[string]map[string][]byte{}}
}

func (s *memoryStore) PutSubagentRun(ctx context.Context, run Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[run.ID] = run
	return nil
}

func (s *memoryStore) GetSubagentRun(ctx context.Context, id string) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[id]
	if !ok {
		return Run{}, errors.New("not found")
	}
	return run, nil
}

func (s *memoryStore) ListSubagentRunsBySession(ctx context.Context, sessionID string, limit int) ([]Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Run, 0, len(s.runs))
	for _, run := range s.runs {
		if sessionID != "" && run.SessionID != sessionID {
			continue
		}
		out = append(out, run)
	}
	return out, nil
}

func (s *memoryStore) ListSubagentRunsByStatus(ctx context.Context, status Status, limit int) ([]Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Run, 0, len(s.runs))
	for _, run := range s.runs {
		if status != "" && run.Status != status {
			continue
		}
		out = append(out, run)
	}
	return out, nil
}

func (s *memoryStore) AppendSubagentEvent(ctx context.Context, event Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
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

func TestManagerQueueLimit(t *testing.T) {
	store := newMemoryStore()
	m := NewManager(Options{Enabled: true, MaxConcurrent: 1, MaxQueue: 1, DefaultTimeout: time.Second, MaxAttempts: 1, NextID: func() string { return "id" + time.Now().Format("150405.000000") }}, store, func(ctx context.Context, run Run) (Result, error) {
		return Result{Summary: "ok"}, nil
	}, nil, nil)
	_, err := m.Enqueue(context.Background(), Request{Task: "one"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Enqueue(context.Background(), Request{Task: "two"}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}
}

func TestManagerRetryThenSuccess(t *testing.T) {
	store := newMemoryStore()
	attempts := 0
	m := NewManager(Options{
		Enabled:        true,
		MaxConcurrent:  1,
		MaxQueue:       8,
		DefaultTimeout: 2 * time.Second,
		MaxAttempts:    2,
		RetryBackoff:   10 * time.Millisecond,
		NextID:         func() string { return "run-retry" },
	}, store, func(ctx context.Context, run Run) (Result, error) {
		attempts++
		if attempts == 1 {
			return Result{}, errors.New("transient")
		}
		return Result{Summary: "done", Output: "done"}, nil
	}, nil, nil)
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	if _, err := m.Enqueue(context.Background(), Request{Task: "retry me"}); err != nil {
		t.Fatal(err)
	}
	runs, err := m.Wait(context.Background(), []string{"run-retry"}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].Status != StatusSucceeded {
		t.Fatalf("expected succeeded, got %s", runs[0].Status)
	}
	if runs[0].Attempt != 2 {
		t.Fatalf("expected attempt 2, got %d", runs[0].Attempt)
	}
}

func TestManagerCancelRunningRun(t *testing.T) {
	store := newMemoryStore()
	m := NewManager(Options{
		Enabled:        true,
		MaxConcurrent:  1,
		MaxQueue:       8,
		DefaultTimeout: 5 * time.Second,
		MaxAttempts:    1,
		NextID:         func() string { return "run-cancel" },
	}, store, func(ctx context.Context, run Run) (Result, error) {
		<-ctx.Done()
		return Result{}, ctx.Err()
	}, nil, nil)
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	if _, err := m.Enqueue(context.Background(), Request{Task: "block"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, err := m.Cancel(context.Background(), "run-cancel"); err != nil {
		t.Fatal(err)
	}
	runs, err := m.Wait(context.Background(), []string{"run-cancel"}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if runs[0].Status != StatusCancelled {
		t.Fatalf("expected cancelled, got %s", runs[0].Status)
	}
}

func TestManagerCancelViaExternalSignal(t *testing.T) {
	store := newMemoryStore()
	started := make(chan struct{}, 1)
	m := NewManager(Options{
		Enabled:        true,
		MaxConcurrent:  1,
		MaxQueue:       8,
		DefaultTimeout: 5 * time.Second,
		MaxAttempts:    1,
		NextID:         func() string { return "run-external-cancel" },
	}, store, func(ctx context.Context, run Run) (Result, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return Result{}, ctx.Err()
	}, nil, nil)
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	if _, err := m.Enqueue(context.Background(), Request{Task: "block"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("run did not start")
	}
	if err := store.PutKV(context.Background(), CancelSignalNamespace, "run-external-cancel", []byte("1")); err != nil {
		t.Fatal(err)
	}
	runs, err := m.Wait(context.Background(), []string{"run-external-cancel"}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if runs[0].Status != StatusCancelled {
		t.Fatalf("expected cancelled, got %s", runs[0].Status)
	}
}

func TestManagerTimeoutStatus(t *testing.T) {
	store := newMemoryStore()
	m := NewManager(Options{
		Enabled:        true,
		MaxConcurrent:  1,
		MaxQueue:       8,
		DefaultTimeout: 20 * time.Millisecond,
		MaxAttempts:    1,
		NextID:         func() string { return "run-timeout" },
	}, store, func(ctx context.Context, run Run) (Result, error) {
		<-ctx.Done()
		return Result{}, ctx.Err()
	}, nil, nil)
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	if _, err := m.Enqueue(context.Background(), Request{Task: "timeout"}); err != nil {
		t.Fatal(err)
	}
	runs, err := m.Wait(context.Background(), []string{"run-timeout"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if runs[0].Status != StatusTimedOut {
		t.Fatalf("expected timed_out, got %s", runs[0].Status)
	}
}

func TestManagerHighThroughputDrainsQueue(t *testing.T) {
	store := newMemoryStore()
	var idCounter atomic.Int64
	m := NewManager(Options{
		Enabled:        true,
		MaxConcurrent:  8,
		MaxQueue:       256,
		DefaultTimeout: 2 * time.Second,
		MaxAttempts:    1,
		NextID: func() string {
			return fmt.Sprintf("run-%d", idCounter.Add(1))
		},
	}, store, func(ctx context.Context, run Run) (Result, error) {
		return Result{Summary: "ok"}, nil
	}, nil, nil)
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	runIDs := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		run, err := m.Enqueue(context.Background(), Request{Task: fmt.Sprintf("task-%d", i)})
		if err != nil {
			t.Fatalf("enqueue failed at %d: %v", i, err)
		}
		runIDs = append(runIDs, run.ID)
	}
	runs, err := m.Wait(context.Background(), runIDs, 8*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 200 {
		t.Fatalf("expected 200 runs, got %d", len(runs))
	}
	for _, run := range runs {
		if run.Status != StatusSucceeded {
			t.Fatalf("expected succeeded, got %s for %s", run.Status, run.ID)
		}
	}
}

func TestManagerLongRunKeepsGoroutinesBounded(t *testing.T) {
	store := newMemoryStore()
	var idCounter atomic.Int64
	before := runtime.NumGoroutine()
	m := NewManager(Options{
		Enabled:        true,
		MaxConcurrent:  4,
		MaxQueue:       512,
		DefaultTimeout: 2 * time.Second,
		MaxAttempts:    1,
		NextID: func() string {
			return fmt.Sprintf("run-long-%d", idCounter.Add(1))
		},
	}, store, func(ctx context.Context, run Run) (Result, error) {
		return Result{Summary: "ok"}, nil
	}, nil, nil)
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	runIDs := make([]string, 0, 500)
	for i := 0; i < 500; i++ {
		run, err := m.Enqueue(context.Background(), Request{Task: fmt.Sprintf("task-%d", i)})
		if err != nil {
			t.Fatalf("enqueue failed at %d: %v", i, err)
		}
		runIDs = append(runIDs, run.ID)
	}
	if _, err := m.Wait(context.Background(), runIDs, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	m.Stop()
	time.Sleep(100 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after > before+20 {
		t.Fatalf("goroutines grew unexpectedly: before=%d after=%d", before, after)
	}
}
