package compaction

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memoryKVStore struct {
	mu sync.Mutex
	kv map[string]map[string][]byte
}

func newMemoryKVStore() *memoryKVStore {
	return &memoryKVStore{kv: map[string]map[string][]byte{}}
}

func (s *memoryKVStore) PutKV(ctx context.Context, namespace, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.kv[namespace]; !ok {
		s.kv[namespace] = map[string][]byte{}
	}
	s.kv[namespace][key] = append([]byte(nil), value...)
	return nil
}

func (s *memoryKVStore) ListKV(ctx context.Context, namespace, prefix string, limit int) (map[string][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ns := s.kv[namespace]
	out := make(map[string][]byte)
	for key, value := range ns {
		out[key] = append([]byte(nil), value...)
	}
	return out, nil
}

func makeTurns(n int) []Turn {
	out := make([]Turn, 0, n)
	now := time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		out = append(out, Turn{
			ID:        fmt.Sprintf("turn-%02d", i),
			Role:      "user",
			Content:   fmt.Sprintf("message-%d", i),
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		})
	}
	return out
}

func TestServiceThresholdActions(t *testing.T) {
	tests := []struct {
		name          string
		aggressivePct int
		emergencyPct  int
		expectAction  string
		expectRemoved int
	}{
		{name: "background", aggressivePct: 90, emergencyPct: 95, expectAction: "background_compaction", expectRemoved: 1},
		{name: "aggressive", aggressivePct: 75, emergencyPct: 95, expectAction: "aggressive_compaction", expectRemoved: 3},
		{name: "emergency", aggressivePct: 75, emergencyPct: 80, expectAction: "emergency_truncate", expectRemoved: 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemoryKVStore()
			turns := makeTurns(8)
			deleted := []string{}
			summarizeCalls := 0
			s := NewService(store, Options{
				Enabled:                true,
				MaxHistoryTurns:        10,
				KeepMinTurns:           2,
				BackgroundThresholdPct: 70,
				AggressiveThresholdPct: tc.aggressivePct,
				EmergencyThresholdPct:  tc.emergencyPct,
				ListTurns: func(ctx context.Context, sessionID string, limit int) ([]Turn, error) {
					return append([]Turn(nil), turns...), nil
				},
				DeleteTurns: func(ctx context.Context, sessionID string, turnIDs []string) error {
					deleted = append(deleted, turnIDs...)
					return nil
				},
				AppendSummaryMarker: func(ctx context.Context, sessionID, summary string) error { return nil },
				Summarize: func(ctx context.Context, sessionID string, removed []Turn) (string, error) {
					summarizeCalls++
					return "", errors.New("force fallback")
				},
			})

			s.compact(context.Background(), "session-1")

			if len(deleted) != tc.expectRemoved {
				t.Fatalf("expected %d removed turns, got %d", tc.expectRemoved, len(deleted))
			}
			runs, err := s.ListRuns(context.Background(), 10)
			if err != nil {
				t.Fatal(err)
			}
			if len(runs) != 1 {
				t.Fatalf("expected 1 run, got %d", len(runs))
			}
			run := runs[0]
			if run.Action != tc.expectAction {
				t.Fatalf("expected action %q, got %q", tc.expectAction, run.Action)
			}
			if run.RemovedTurns != tc.expectRemoved {
				t.Fatalf("expected removed turns %d, got %d", tc.expectRemoved, run.RemovedTurns)
			}
			if !strings.Contains(run.Summary, "Compaction condensed earlier turns") {
				t.Fatalf("expected deterministic fallback summary, got %q", run.Summary)
			}
			if tc.expectAction == "emergency_truncate" && summarizeCalls != 0 {
				t.Fatalf("emergency compaction should skip summarize callback, got %d calls", summarizeCalls)
			}
			if tc.expectAction != "emergency_truncate" && summarizeCalls == 0 {
				t.Fatal("expected summarize callback to be attempted")
			}
		})
	}
}

func TestServiceObserveTurnSingleActivePerSession(t *testing.T) {
	store := newMemoryKVStore()
	var listCalls atomic.Int32
	ready := make(chan struct{})
	release := make(chan struct{})
	deleteDone := make(chan struct{}, 1)

	s := NewService(store, Options{
		Enabled:                true,
		MaxHistoryTurns:        10,
		KeepMinTurns:           2,
		BackgroundThresholdPct: 10,
		AggressiveThresholdPct: 80,
		EmergencyThresholdPct:  95,
		ListTurns: func(ctx context.Context, sessionID string, limit int) ([]Turn, error) {
			if listCalls.Add(1) == 1 {
				close(ready)
			}
			<-release
			return makeTurns(8), nil
		},
		DeleteTurns: func(ctx context.Context, sessionID string, turnIDs []string) error {
			select {
			case deleteDone <- struct{}{}:
			default:
			}
			return nil
		},
		AppendSummaryMarker: func(ctx context.Context, sessionID, summary string) error { return nil },
	})

	s.ObserveTurn(context.Background(), "session-1")
	<-ready
	s.ObserveTurn(context.Background(), "session-1")
	close(release)

	select {
	case <-deleteDone:
	case <-time.After(2 * time.Second):
		t.Fatal("expected compaction run to finish")
	}
	if got := listCalls.Load(); got != 1 {
		t.Fatalf("expected one active compaction call, got %d", got)
	}
}

func TestServiceListRunsSortedAndLimited(t *testing.T) {
	store := newMemoryKVStore()
	now := time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
	s := NewService(store, Options{Enabled: true})
	if err := s.persistRun(context.Background(), CompactionRun{SessionID: "s", Action: "a", CreatedAt: now.Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := s.persistRun(context.Background(), CompactionRun{SessionID: "s", Action: "b", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutKV(context.Background(), kvNamespaceRuns, "bad", []byte("{")); err != nil {
		t.Fatal(err)
	}

	runs, err := s.ListRuns(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].Action != "b" {
		t.Fatalf("expected newest run first, got %q", runs[0].Action)
	}
}

func TestDeterministicSummaryFallbackDefaults(t *testing.T) {
	summary := deterministicSummary(nil, 5)
	if !strings.Contains(summary, "removed old conversation turns") {
		t.Fatalf("unexpected summary: %q", summary)
	}
}
