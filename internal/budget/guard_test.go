package budget

import (
	"context"
	"testing"

	"github.com/grixate/squidbot/internal/telemetry"
)

type memoryStore struct {
	counters     map[string]Counter
	reservations map[string]Reservation
	nextID       int
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		counters:     map[string]Counter{},
		reservations: map[string]Reservation{},
	}
}

func (m *memoryStore) GetBudgetCounter(ctx context.Context, scope string) (Counter, error) {
	if counter, ok := m.counters[scope]; ok {
		return counter, nil
	}
	return Counter{Scope: scope}, nil
}

func (m *memoryStore) AddBudgetUsage(ctx context.Context, scope string, prompt, completion, total uint64) error {
	counter := m.counters[scope]
	counter.Scope = scope
	counter.PromptTokens += prompt
	counter.CompletionTokens += completion
	counter.TotalTokens += total
	m.counters[scope] = counter
	return nil
}

func (m *memoryStore) ReserveBudget(ctx context.Context, scope string, tokens uint64, ttlSec int) (reservationID string, err error) {
	m.nextID++
	id := "r-" + string(rune('a'+m.nextID))
	m.reservations[id] = Reservation{ID: id, Scope: scope, Tokens: tokens}
	counter := m.counters[scope]
	counter.Scope = scope
	counter.ReservedTokens += tokens
	m.counters[scope] = counter
	return id, nil
}

func (m *memoryStore) FinalizeBudgetReservation(ctx context.Context, reservationID string, actualTotal uint64) error {
	res := m.reservations[reservationID]
	counter := m.counters[res.Scope]
	if counter.ReservedTokens >= res.Tokens {
		counter.ReservedTokens -= res.Tokens
	} else {
		counter.ReservedTokens = 0
	}
	counter.TotalTokens += actualTotal
	m.counters[res.Scope] = counter
	res.Finalized = true
	m.reservations[reservationID] = res
	return nil
}

func (m *memoryStore) CancelBudgetReservation(ctx context.Context, reservationID string) error {
	res := m.reservations[reservationID]
	counter := m.counters[res.Scope]
	if counter.ReservedTokens >= res.Tokens {
		counter.ReservedTokens -= res.Tokens
	} else {
		counter.ReservedTokens = 0
	}
	m.counters[res.Scope] = counter
	res.Cancelled = true
	m.reservations[reservationID] = res
	return nil
}

func TestGuardBlocksWhenHardLimitExceeded(t *testing.T) {
	store := newMemoryStore()
	store.counters["global"] = Counter{Scope: "global", TotalTokens: 9}
	guard := NewGuard(store, &telemetry.Metrics{})
	settings := Settings{
		Enabled:                true,
		Mode:                   ModeHybrid,
		GlobalHardLimitTokens:  10,
		GlobalSoftThresholdPct: 80,
		EstimateOnMissingUsage: true,
		EstimateCharsPerToken:  4,
		ReservationTTLSec:      60,
	}.Normalized()
	_, err := guard.Preflight(context.Background(), settings, []ScopeLimit{{
		Key:              "global",
		HardLimitTokens:  settings.GlobalHardLimitTokens,
		SoftThresholdPct: settings.GlobalSoftThresholdPct,
	}}, 2)
	if err == nil {
		t.Fatal("expected hard limit error")
	}
	if _, ok := err.(*LimitError); !ok {
		t.Fatalf("expected LimitError, got %T", err)
	}
}

func TestGuardCommitEstimatesMissingUsage(t *testing.T) {
	store := newMemoryStore()
	guard := NewGuard(store, &telemetry.Metrics{})
	settings := Settings{
		Enabled:                true,
		Mode:                   ModeHybrid,
		GlobalHardLimitTokens:  100,
		GlobalSoftThresholdPct: 80,
		EstimateOnMissingUsage: true,
		EstimateCharsPerToken:  4,
		ReservationTTLSec:      60,
	}.Normalized()
	scopes := []ScopeLimit{{
		Key:              "global",
		HardLimitTokens:  settings.GlobalHardLimitTokens,
		SoftThresholdPct: settings.GlobalSoftThresholdPct,
	}}
	preflight, err := guard.Preflight(context.Background(), settings, scopes, 5)
	if err != nil {
		t.Fatal(err)
	}
	result, err := guard.Commit(context.Background(), settings, scopes, preflight, Usage{OutputChars: 9})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Estimated {
		t.Fatal("expected estimated usage")
	}
	if result.TotalTokens != 3 {
		t.Fatalf("expected estimated total 3, got %d", result.TotalTokens)
	}
	counter, err := store.GetBudgetCounter(context.Background(), "global")
	if err != nil {
		t.Fatal(err)
	}
	if counter.TotalTokens != 3 {
		t.Fatalf("expected counter total 3, got %d", counter.TotalTokens)
	}
}
