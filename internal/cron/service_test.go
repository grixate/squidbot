package cron

import (
	"testing"
	"time"
)

func TestComputeNextRunEvery(t *testing.T) {
	now := time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC)
	next := computeNextRun(JobSchedule{Kind: ScheduleEvery, Every: 30_000}, now)
	if next == nil {
		t.Fatal("next run should not be nil")
	}
	if next.Sub(now) != 30*time.Second {
		t.Fatalf("unexpected interval: %s", next.Sub(now))
	}
}

func TestComputeNextRunCron(t *testing.T) {
	now := time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC)
	next := computeNextRun(JobSchedule{Kind: ScheduleCron, Expr: "*/5 * * * *"}, now)
	if next == nil {
		t.Fatal("next run should not be nil")
	}
	if next.Minute()%5 != 0 {
		t.Fatalf("minute should be divisible by 5: %d", next.Minute())
	}
}
