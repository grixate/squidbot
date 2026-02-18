package mission

import (
	"testing"
	"time"
)

func TestDefaultColumns(t *testing.T) {
	now := time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
	columns := DefaultColumns(now)
	if len(columns) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(columns))
	}
	if columns[0].ID != ColumnBacklog || columns[1].ID != ColumnInProgress || columns[2].ID != ColumnBlocked || columns[3].ID != ColumnDone {
		t.Fatalf("unexpected default column order: %+v", columns)
	}
	if columns[0].CreatedAt != now || columns[3].UpdatedAt != now {
		t.Fatalf("expected provided timestamp in default columns: %+v", columns)
	}

	fallback := DefaultColumns(time.Time{})
	for _, col := range fallback {
		if col.CreatedAt.IsZero() || col.UpdatedAt.IsZero() {
			t.Fatalf("expected non-zero fallback timestamps: %+v", col)
		}
	}
}

func TestTaskAutomationPolicyDefaultsAndMatrix(t *testing.T) {
	now := time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
	policy := DefaultTaskAutomationPolicy(now)
	if !policy.EnableChat || !policy.EnableHeartbeat || !policy.EnableCron || !policy.EnableSubagent {
		t.Fatalf("unexpected default automation flags: %+v", policy)
	}
	if policy.DefaultColumnID != ColumnBacklog {
		t.Fatalf("unexpected default column id: %s", policy.DefaultColumnID)
	}
	if policy.DedupeWindow() != 6*time.Hour {
		t.Fatalf("unexpected dedupe window: %s", policy.DedupeWindow())
	}

	policy.DedupeWindowSec = 0
	if policy.DedupeWindow() != 6*time.Hour {
		t.Fatalf("expected fallback dedupe window, got %s", policy.DedupeWindow())
	}

	policy.EnableChat = false
	policy.EnableHeartbeat = false
	policy.EnableCron = false
	policy.EnableSubagent = false
	if policy.EnabledForSource(TaskSourceChat) {
		t.Fatal("chat should be disabled")
	}
	if policy.EnabledForSource(TaskSourceHeartbeat) {
		t.Fatal("heartbeat should be disabled")
	}
	if policy.EnabledForSource(TaskSourceCron) {
		t.Fatal("cron should be disabled")
	}
	if policy.EnabledForSource(TaskSourceSubagent) {
		t.Fatal("subagent should be disabled")
	}
	if !policy.EnabledForSource(TaskSourceManual) {
		t.Fatal("manual source should always be enabled")
	}
}

func TestNormalizeTaskTitle(t *testing.T) {
	if got := NormalizeTaskTitle("  Ship  v2.0!!!  now  "); got != "ship v2 0 now" {
		t.Fatalf("unexpected normalized title: %q", got)
	}
	if got := NormalizeTaskTitle("\t\n"); got != "" {
		t.Fatalf("expected empty normalized title, got %q", got)
	}
}

func TestNormalizePriority(t *testing.T) {
	cases := map[string]string{
		"critical": "critical",
		" HIGH ":   "high",
		"Medium":   "medium",
		"low":      "low",
		"urgent":   "",
		"":         "",
	}
	for in, want := range cases {
		if got := NormalizePriority(in); got != want {
			t.Fatalf("NormalizePriority(%q)=%q want=%q", in, got, want)
		}
	}
}
