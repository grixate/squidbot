package bbolt

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/budget"
	"go.etcd.io/bbolt"
)

const tokenSafetyOverrideKey = "override"

type budgetEvent struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Scope     string         `json:"scope,omitempty"`
	Message   string         `json:"message,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

func budgetCounterKey(scope string) string {
	return "counter:" + strings.TrimSpace(scope)
}

func budgetReservationKey(id string) string {
	return "reservation:" + strings.TrimSpace(id)
}

func budgetEventKey(id string) string {
	return "event:" + strings.TrimSpace(id)
}

func (s *Store) GetTokenSafetyOverride(_ context.Context) (budget.TokenSafetyOverride, error) {
	var out budget.TokenSafetyOverride
	err := s.db.View(func(tx *bbolt.Tx) error {
		raw := tx.Bucket(bucketTokenSafetyOverride).Get([]byte(tokenSafetyOverrideKey))
		if len(raw) == 0 {
			return budget.ErrNotFound
		}
		return json.Unmarshal(raw, &out)
	})
	if err != nil {
		return budget.TokenSafetyOverride{}, err
	}
	return out, nil
}

func (s *Store) PutTokenSafetyOverride(ctx context.Context, override budget.TokenSafetyOverride) error {
	if override.UpdatedAt.IsZero() {
		override.UpdatedAt = time.Now().UTC()
	}
	if override.Version <= 0 {
		override.Version = 1
	}
	override.Settings = override.Settings.Normalized()
	raw, err := json.Marshal(override)
	if err != nil {
		return err
	}
	return s.runWrite(ctx, func(tx *bbolt.Tx) error {
		if err := tx.Bucket(bucketTokenSafetyOverride).Put([]byte(tokenSafetyOverrideKey), raw); err != nil {
			return err
		}
		return s.putBudgetEventTx(tx, budgetEvent{
			ID:        s.nextULID(),
			Kind:      "token_safety_override_updated",
			CreatedAt: time.Now().UTC(),
		})
	})
}

func (s *Store) GetBudgetCounter(_ context.Context, scope string) (budget.Counter, error) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return budget.Counter{}, fmt.Errorf("scope is required")
	}
	var out budget.Counter
	err := s.db.View(func(tx *bbolt.Tx) error {
		raw := tx.Bucket(bucketBudgetCounters).Get([]byte(budgetCounterKey(scope)))
		if len(raw) == 0 {
			out = budget.Counter{Scope: scope}
			return nil
		}
		return json.Unmarshal(raw, &out)
	})
	if err != nil {
		return budget.Counter{}, err
	}
	if strings.TrimSpace(out.Scope) == "" {
		out.Scope = scope
	}
	return out, nil
}

func (s *Store) AddBudgetUsage(ctx context.Context, scope string, prompt, completion, total uint64) error {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return fmt.Errorf("scope is required")
	}
	now := time.Now().UTC()
	return s.runWrite(ctx, func(tx *bbolt.Tx) error {
		counter, err := s.getBudgetCounterTx(tx, scope)
		if err != nil {
			return err
		}
		counter.PromptTokens += prompt
		counter.CompletionTokens += completion
		counter.TotalTokens += total
		counter.UpdatedAt = now
		if err := s.putBudgetCounterTx(tx, counter); err != nil {
			return err
		}
		return s.putBudgetEventTx(tx, budgetEvent{
			ID:        s.nextULID(),
			Kind:      "budget_usage_added",
			Scope:     scope,
			CreatedAt: now,
			Metadata: map[string]any{
				"prompt_tokens":     prompt,
				"completion_tokens": completion,
				"total_tokens":      total,
			},
		})
	})
}

func (s *Store) ReserveBudget(ctx context.Context, scope string, tokens uint64, ttlSec int) (string, error) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return "", fmt.Errorf("scope is required")
	}
	if tokens == 0 {
		return "", fmt.Errorf("tokens must be greater than 0")
	}
	if ttlSec <= 0 {
		ttlSec = 300
	}
	now := time.Now().UTC()
	reservationID := s.nextULID()
	reservation := budget.Reservation{
		ID:        reservationID,
		Scope:     scope,
		Tokens:    tokens,
		CreatedAt: now,
		ExpiresAt: now.Add(time.Duration(ttlSec) * time.Second),
	}
	return reservationID, s.runWrite(ctx, func(tx *bbolt.Tx) error {
		counter, err := s.getBudgetCounterTx(tx, scope)
		if err != nil {
			return err
		}
		counter.ReservedTokens += tokens
		counter.UpdatedAt = now
		if err := s.putBudgetCounterTx(tx, counter); err != nil {
			return err
		}
		raw, err := json.Marshal(reservation)
		if err != nil {
			return err
		}
		if err := tx.Bucket(bucketBudgetReservations).Put([]byte(budgetReservationKey(reservationID)), raw); err != nil {
			return err
		}
		return s.putBudgetEventTx(tx, budgetEvent{
			ID:        s.nextULID(),
			Kind:      "budget_reserved",
			Scope:     scope,
			CreatedAt: now,
			Metadata: map[string]any{
				"reservation_id": reservationID,
				"tokens":         tokens,
			},
		})
	})
}

func (s *Store) FinalizeBudgetReservation(ctx context.Context, reservationID string, actualTotal uint64) error {
	reservationID = strings.TrimSpace(reservationID)
	if reservationID == "" {
		return fmt.Errorf("reservationID is required")
	}
	now := time.Now().UTC()
	return s.runWrite(ctx, func(tx *bbolt.Tx) error {
		reservation, err := s.getBudgetReservationTx(tx, reservationID)
		if err != nil {
			return err
		}
		if reservation.Finalized || reservation.Cancelled {
			return nil
		}
		counter, err := s.getBudgetCounterTx(tx, reservation.Scope)
		if err != nil {
			return err
		}
		if counter.ReservedTokens >= reservation.Tokens {
			counter.ReservedTokens -= reservation.Tokens
		} else {
			counter.ReservedTokens = 0
		}
		counter.TotalTokens += actualTotal
		counter.UpdatedAt = now
		if err := s.putBudgetCounterTx(tx, counter); err != nil {
			return err
		}
		reservation.Finalized = true
		reservation.FinalizedAt = now
		raw, err := json.Marshal(reservation)
		if err != nil {
			return err
		}
		if err := tx.Bucket(bucketBudgetReservations).Put([]byte(budgetReservationKey(reservation.ID)), raw); err != nil {
			return err
		}
		return s.putBudgetEventTx(tx, budgetEvent{
			ID:        s.nextULID(),
			Kind:      "budget_reservation_finalized",
			Scope:     reservation.Scope,
			CreatedAt: now,
			Metadata: map[string]any{
				"reservation_id": reservation.ID,
				"actual_total":   actualTotal,
			},
		})
	})
}

func (s *Store) CancelBudgetReservation(ctx context.Context, reservationID string) error {
	reservationID = strings.TrimSpace(reservationID)
	if reservationID == "" {
		return fmt.Errorf("reservationID is required")
	}
	now := time.Now().UTC()
	return s.runWrite(ctx, func(tx *bbolt.Tx) error {
		reservation, err := s.getBudgetReservationTx(tx, reservationID)
		if err != nil {
			return err
		}
		if reservation.Cancelled || reservation.Finalized {
			return nil
		}
		counter, err := s.getBudgetCounterTx(tx, reservation.Scope)
		if err != nil {
			return err
		}
		if counter.ReservedTokens >= reservation.Tokens {
			counter.ReservedTokens -= reservation.Tokens
		} else {
			counter.ReservedTokens = 0
		}
		counter.UpdatedAt = now
		if err := s.putBudgetCounterTx(tx, counter); err != nil {
			return err
		}
		reservation.Cancelled = true
		reservation.FinalizedAt = now
		raw, err := json.Marshal(reservation)
		if err != nil {
			return err
		}
		if err := tx.Bucket(bucketBudgetReservations).Put([]byte(budgetReservationKey(reservation.ID)), raw); err != nil {
			return err
		}
		return s.putBudgetEventTx(tx, budgetEvent{
			ID:        s.nextULID(),
			Kind:      "budget_reservation_cancelled",
			Scope:     reservation.Scope,
			CreatedAt: now,
			Metadata: map[string]any{
				"reservation_id": reservation.ID,
			},
		})
	})
}

func (s *Store) ListBudgetReservations(_ context.Context, scope string, limit int) ([]budget.Reservation, error) {
	scope = strings.TrimSpace(scope)
	if limit <= 0 {
		limit = 100
	}
	out := make([]budget.Reservation, 0, limit)
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketBudgetReservations).ForEach(func(_, value []byte) error {
			var reservation budget.Reservation
			if err := json.Unmarshal(value, &reservation); err != nil {
				return nil
			}
			if scope != "" && reservation.Scope != scope {
				return nil
			}
			out = append(out, reservation)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) getBudgetCounterTx(tx *bbolt.Tx, scope string) (budget.Counter, error) {
	scope = strings.TrimSpace(scope)
	counter := budget.Counter{Scope: scope}
	raw := tx.Bucket(bucketBudgetCounters).Get([]byte(budgetCounterKey(scope)))
	if len(raw) == 0 {
		return counter, nil
	}
	if err := json.Unmarshal(raw, &counter); err != nil {
		return budget.Counter{}, err
	}
	if strings.TrimSpace(counter.Scope) == "" {
		counter.Scope = scope
	}
	return counter, nil
}

func (s *Store) putBudgetCounterTx(tx *bbolt.Tx, counter budget.Counter) error {
	counter.Scope = strings.TrimSpace(counter.Scope)
	if counter.Scope == "" {
		return fmt.Errorf("counter scope is required")
	}
	raw, err := json.Marshal(counter)
	if err != nil {
		return err
	}
	return tx.Bucket(bucketBudgetCounters).Put([]byte(budgetCounterKey(counter.Scope)), raw)
}

func (s *Store) getBudgetReservationTx(tx *bbolt.Tx, reservationID string) (budget.Reservation, error) {
	var reservation budget.Reservation
	raw := tx.Bucket(bucketBudgetReservations).Get([]byte(budgetReservationKey(reservationID)))
	if len(raw) == 0 {
		return budget.Reservation{}, budget.ErrNotFound
	}
	if err := json.Unmarshal(raw, &reservation); err != nil {
		return budget.Reservation{}, err
	}
	return reservation, nil
}

func (s *Store) putBudgetEventTx(tx *bbolt.Tx, event budgetEvent) error {
	if strings.TrimSpace(event.ID) == "" {
		event.ID = s.nextULID()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return tx.Bucket(bucketBudgetEvents).Put([]byte(budgetEventKey(event.ID)), raw)
}
