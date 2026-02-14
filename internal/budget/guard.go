package budget

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/grixate/squidbot/internal/telemetry"
)

type Store interface {
	GetBudgetCounter(ctx context.Context, scope string) (Counter, error)
	AddBudgetUsage(ctx context.Context, scope string, prompt, completion, total uint64) error
	ReserveBudget(ctx context.Context, scope string, tokens uint64, ttlSec int) (reservationID string, err error)
	FinalizeBudgetReservation(ctx context.Context, reservationID string, actualTotal uint64) error
	CancelBudgetReservation(ctx context.Context, reservationID string) error
}

type Guard struct {
	store   Store
	metrics *telemetry.Metrics
	mu      sync.Mutex
}

func NewGuard(store Store, metrics *telemetry.Metrics) *Guard {
	return &Guard{store: store, metrics: metrics}
}

func (g *Guard) Preflight(ctx context.Context, settings Settings, scopes []ScopeLimit, plannedMaxTokens uint64) (PreflightResult, error) {
	result := PreflightResult{Reservations: map[string]string{}}
	if g == nil || g.store == nil {
		return result, nil
	}
	settings = settings.Normalized()
	if plannedMaxTokens == 0 {
		plannedMaxTokens = 1
	}
	if !settings.Enabled {
		if g.metrics != nil {
			g.metrics.TokenSafetyDisabledBypass.Add(1)
		}
		return result, nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	created := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		key := strings.TrimSpace(scope.Key)
		if key == "" {
			continue
		}
		counter, err := g.store.GetBudgetCounter(ctx, key)
		if err != nil && !errors.Is(err, ErrNotFound) {
			g.abortByIDs(ctx, created)
			return PreflightResult{}, err
		}
		limit := scope.HardLimitTokens
		if limit > 0 {
			next := counter.TotalTokens + counter.ReservedTokens + plannedMaxTokens
			if next > limit {
				g.abortByIDs(ctx, created)
				if g.metrics != nil {
					g.metrics.TokenSafetyPreflightBlocked.Add(1)
				}
				return PreflightResult{}, &LimitError{
					Scope:     key,
					Used:      counter.TotalTokens,
					Reserved:  counter.ReservedTokens,
					Requested: plannedMaxTokens,
					Limit:     limit,
				}
			}
			if softWarningReached(counter.TotalTokens, counter.ReservedTokens, plannedMaxTokens, limit, scope.SoftThresholdPct) {
				result.Warnings = append(result.Warnings, Warning{
					Scope:        key,
					Used:         counter.TotalTokens,
					Reserved:     counter.ReservedTokens + plannedMaxTokens,
					HardLimit:    limit,
					ThresholdPct: scope.SoftThresholdPct,
				})
			}
		}
		reservationID, err := g.store.ReserveBudget(ctx, key, plannedMaxTokens, settings.ReservationTTLSec)
		if err != nil {
			g.abortByIDs(ctx, created)
			return PreflightResult{}, err
		}
		result.Reservations[key] = reservationID
		created = append(created, reservationID)
	}
	if g.metrics != nil {
		g.metrics.TokenSafetyPreflightAllowed.Add(1)
		if len(result.Warnings) > 0 {
			g.metrics.TokenSafetySoftWarnings.Add(uint64(len(result.Warnings)))
		}
	}
	return result, nil
}

func (g *Guard) Commit(ctx context.Context, settings Settings, scopes []ScopeLimit, preflight PreflightResult, usage Usage) (CommitResult, error) {
	result := CommitResult{}
	if g == nil || g.store == nil {
		return result, nil
	}
	settings = settings.Normalized()

	prompt := uint64(maxInt(usage.PromptTokens, 0))
	completion := uint64(maxInt(usage.CompletionTokens, 0))
	total := uint64(maxInt(usage.TotalTokens, 0))
	if total == 0 && prompt+completion > 0 {
		total = prompt + completion
	}
	if total == 0 && settings.EstimateOnMissingUsage {
		outputChars := maxInt(usage.OutputChars, 1)
		total = ceilDiv(uint64(outputChars), uint64(maxInt(settings.EstimateCharsPerToken, 1)))
		result.Estimated = true
		result.EstimatedTokens = total
		if g.metrics != nil {
			g.metrics.TokenSafetyEstimatedUsage.Add(1)
		}
	}
	result.TotalTokens = total

	seen := map[string]struct{}{}
	for _, scope := range scopes {
		key := strings.TrimSpace(scope.Key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		reservationID := strings.TrimSpace(preflight.Reservations[key])
		if reservationID != "" {
			if err := g.store.FinalizeBudgetReservation(ctx, reservationID, total); err != nil {
				return result, err
			}
			if prompt > 0 || completion > 0 {
				if err := g.store.AddBudgetUsage(ctx, key, prompt, completion, 0); err != nil {
					return result, err
				}
			}
		} else {
			if prompt > 0 || completion > 0 || total > 0 {
				if err := g.store.AddBudgetUsage(ctx, key, prompt, completion, total); err != nil {
					return result, err
				}
			}
		}
		counter, err := g.store.GetBudgetCounter(ctx, key)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return result, err
		}
		if settings.Enabled && scope.HardLimitTokens > 0 && softWarningReached(counter.TotalTokens, counter.ReservedTokens, 0, scope.HardLimitTokens, scope.SoftThresholdPct) {
			result.Warnings = append(result.Warnings, Warning{
				Scope:        key,
				Used:         counter.TotalTokens,
				Reserved:     counter.ReservedTokens,
				HardLimit:    scope.HardLimitTokens,
				ThresholdPct: scope.SoftThresholdPct,
			})
		}
	}
	if g.metrics != nil && len(result.Warnings) > 0 {
		g.metrics.TokenSafetySoftWarnings.Add(uint64(len(result.Warnings)))
	}
	return result, nil
}

func (g *Guard) Abort(ctx context.Context, preflight PreflightResult) {
	if g == nil || g.store == nil {
		return
	}
	for _, reservationID := range preflight.Reservations {
		if strings.TrimSpace(reservationID) == "" {
			continue
		}
		_ = g.store.CancelBudgetReservation(ctx, reservationID)
	}
}

func (g *Guard) abortByIDs(ctx context.Context, ids []string) {
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			continue
		}
		_ = g.store.CancelBudgetReservation(ctx, id)
	}
}

func softWarningReached(used, reserved, requested, hard uint64, thresholdPct int) bool {
	if hard == 0 || thresholdPct <= 0 {
		return false
	}
	if thresholdPct > 100 {
		thresholdPct = 100
	}
	next := used + reserved + requested
	return next*100 >= hard*uint64(thresholdPct)
}

func ceilDiv(a, b uint64) uint64 {
	if b == 0 {
		return a
	}
	return (a + b - 1) / b
}

func maxInt(v, floor int) int {
	if v < floor {
		return floor
	}
	return v
}
