package bbolt

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/budget"
)

func TestTokenSafetyOverrideRoundTrip(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "budget-override.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	_, err = store.GetTokenSafetyOverride(ctx)
	if !errors.Is(err, budget.ErrNotFound) {
		t.Fatalf("expected ErrNotFound before override, got %v", err)
	}

	override := budget.TokenSafetyOverride{
		Settings: budget.Settings{
			Enabled:                    true,
			Mode:                       budget.ModeHybrid,
			GlobalHardLimitTokens:      1234,
			SessionHardLimitTokens:     321,
			SubagentRunHardLimitTokens: 111,
			EstimateOnMissingUsage:     true,
			EstimateCharsPerToken:      4,
			TrustedWriters:             []string{"cli:user"},
		},
		UpdatedAt: time.Now().UTC(),
		Version:   1,
	}
	if err := store.PutTokenSafetyOverride(ctx, override); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetTokenSafetyOverride(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Settings.GlobalHardLimitTokens != 1234 {
		t.Fatalf("unexpected global limit: %d", got.Settings.GlobalHardLimitTokens)
	}
	if got.Settings.Mode != budget.ModeHybrid {
		t.Fatalf("unexpected mode: %s", got.Settings.Mode)
	}
}

func TestBudgetReserveFinalizeAndCancel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "budget-reserve.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	scope := "global"

	reservationID, err := store.ReserveBudget(ctx, scope, 100, 60)
	if err != nil {
		t.Fatal(err)
	}
	counter, err := store.GetBudgetCounter(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if counter.ReservedTokens != 100 {
		t.Fatalf("expected reserved=100, got %d", counter.ReservedTokens)
	}

	if err := store.FinalizeBudgetReservation(ctx, reservationID, 60); err != nil {
		t.Fatal(err)
	}
	counter, err = store.GetBudgetCounter(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if counter.ReservedTokens != 0 {
		t.Fatalf("expected reserved reset after finalize, got %d", counter.ReservedTokens)
	}
	if counter.TotalTokens != 60 {
		t.Fatalf("expected total=60, got %d", counter.TotalTokens)
	}

	reservationID2, err := store.ReserveBudget(ctx, scope, 40, 60)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CancelBudgetReservation(ctx, reservationID2); err != nil {
		t.Fatal(err)
	}
	counter, err = store.GetBudgetCounter(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if counter.ReservedTokens != 0 {
		t.Fatalf("expected reserved reset after cancel, got %d", counter.ReservedTokens)
	}
	if err := store.AddBudgetUsage(ctx, scope, 10, 5, 15); err != nil {
		t.Fatal(err)
	}
	counter, err = store.GetBudgetCounter(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if counter.PromptTokens != 10 || counter.CompletionTokens != 5 {
		t.Fatalf("unexpected prompt/completion totals: %+v", counter)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	counter, err = store.GetBudgetCounter(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if counter.TotalTokens != 75 {
		t.Fatalf("expected total usage persisted across restart, got %d", counter.TotalTokens)
	}
}
