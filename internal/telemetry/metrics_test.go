package telemetry

import "testing"

func TestMetricsSnapshotClampsNegativeGauges(t *testing.T) {
	m := &Metrics{}
	m.ActiveActors.Add(-5)
	m.ActiveTurns.Add(-2)
	m.CronRunning.Add(-9)
	m.InboundCount.Add(7)

	s := m.Snapshot()
	if s["active_actors"] != 0 {
		t.Fatalf("expected clamped active_actors=0, got %d", s["active_actors"])
	}
	if s["active_turns"] != 0 {
		t.Fatalf("expected clamped active_turns=0, got %d", s["active_turns"])
	}
	if s["cron_running"] != 0 {
		t.Fatalf("expected clamped cron_running=0, got %d", s["cron_running"])
	}
	if s["inbound_count"] != 7 {
		t.Fatalf("expected inbound_count=7, got %d", s["inbound_count"])
	}
}
