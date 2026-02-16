package cron

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/telemetry"
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

func TestServiceWorkerPoolRunsConcurrentJobs(t *testing.T) {
	store := newCronMemoryStore()
	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		job := Job{
			ID:      "job-concurrent-" + string(rune('a'+i)),
			Name:    "concurrent",
			Enabled: true,
			Schedule: JobSchedule{
				Kind:  ScheduleEvery,
				Every: 1000,
			},
			State: JobState{
				NextRunAt: timePtr(now.Add(-time.Second)),
			},
		}
		if err := store.putJob(job); err != nil {
			t.Fatal(err)
		}
	}

	var running atomic.Int64
	var maxRunning atomic.Int64
	var executions atomic.Int64
	service := NewService(store, func(ctx context.Context, job Job) (string, error) {
		current := running.Add(1)
		for {
			previous := maxRunning.Load()
			if current <= previous || maxRunning.CompareAndSwap(previous, current) {
				break
			}
		}
		defer running.Add(-1)
		time.Sleep(120 * time.Millisecond)
		executions.Add(1)
		return "ok", nil
	}, &telemetry.Metrics{}, Options{
		Enabled:       true,
		TickInterval:  10 * time.Millisecond,
		MaxConcurrent: 2,
		MaxQueue:      8,
	})
	service.Start()
	defer service.Stop()

	waitFor(t, 2*time.Second, func() bool { return executions.Load() >= 2 })
	if maxRunning.Load() < 2 {
		t.Fatalf("expected concurrent execution, maxRunning=%d", maxRunning.Load())
	}
}

func TestServiceQueueFullLeavesJobsDue(t *testing.T) {
	store := newCronMemoryStore()
	now := time.Now().UTC()
	due := now.Add(-2 * time.Second)
	ids := []string{"job-full-1", "job-full-2", "job-full-3"}
	for _, id := range ids {
		job := Job{
			ID:      id,
			Name:    id,
			Enabled: true,
			Schedule: JobSchedule{
				Kind:  ScheduleEvery,
				Every: 1000,
			},
			State: JobState{
				NextRunAt: timePtr(due),
			},
		}
		if err := store.putJob(job); err != nil {
			t.Fatal(err)
		}
	}

	metrics := &telemetry.Metrics{}
	service := NewService(store, func(ctx context.Context, job Job) (string, error) {
		time.Sleep(500 * time.Millisecond)
		return "ok", nil
	}, metrics, Options{
		Enabled:       true,
		TickInterval:  10 * time.Millisecond,
		MaxConcurrent: 1,
		MaxQueue:      1,
	})
	service.Start()
	defer service.Stop()

	waitFor(t, 2*time.Second, func() bool { return metrics.CronQueueFull.Load() > 0 })

	untouched := 0
	for _, id := range ids {
		job, err := service.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if job.State.LastRunAt == nil && job.State.NextRunAt != nil && job.State.NextRunAt.Equal(due) {
			untouched++
		}
	}
	if untouched == 0 {
		t.Fatal("expected at least one due job to remain untouched when queue is full")
	}
}

func TestServiceAtScheduleDisablesOnDispatch(t *testing.T) {
	store := newCronMemoryStore()
	now := time.Now().UTC()
	at := now.Add(-time.Minute)
	job := Job{
		ID:      "job-at",
		Name:    "at",
		Enabled: true,
		Schedule: JobSchedule{
			Kind: ScheduleAt,
			At:   &at,
		},
		State: JobState{
			NextRunAt: timePtr(now.Add(-time.Second)),
		},
	}
	if err := store.putJob(job); err != nil {
		t.Fatal(err)
	}

	blocker := make(chan struct{})
	service := NewService(store, func(ctx context.Context, job Job) (string, error) {
		<-blocker
		return "ok", nil
	}, &telemetry.Metrics{}, Options{
		Enabled:       true,
		TickInterval:  10 * time.Millisecond,
		MaxConcurrent: 1,
		MaxQueue:      2,
	})
	service.Start()

	waitFor(t, time.Second, func() bool {
		updated, err := service.Get(context.Background(), "job-at")
		if err != nil {
			return false
		}
		return !updated.Enabled && updated.State.LastRunAt != nil
	})

	close(blocker)
	service.Stop()
}

func TestServiceMissedRecurringRunsOnceThenResumesCadence(t *testing.T) {
	store := newCronMemoryStore()
	now := time.Now().UTC()
	job := Job{
		ID:      "job-missed",
		Name:    "missed",
		Enabled: true,
		Schedule: JobSchedule{
			Kind:  ScheduleEvery,
			Every: 1000,
		},
		State: JobState{
			NextRunAt: timePtr(now.Add(-10 * time.Second)),
		},
	}
	if err := store.putJob(job); err != nil {
		t.Fatal(err)
	}

	var executions atomic.Int64
	service := NewService(store, func(ctx context.Context, job Job) (string, error) {
		executions.Add(1)
		return "ok", nil
	}, &telemetry.Metrics{}, Options{
		Enabled:       true,
		TickInterval:  10 * time.Millisecond,
		MaxConcurrent: 1,
		MaxQueue:      4,
	})
	service.Start()
	defer service.Stop()

	waitFor(t, time.Second, func() bool { return executions.Load() >= 1 })
	time.Sleep(200 * time.Millisecond)
	if executions.Load() != 1 {
		t.Fatalf("expected one immediate catch-up run, got %d", executions.Load())
	}

	updated, err := service.Get(context.Background(), "job-missed")
	if err != nil {
		t.Fatal(err)
	}
	if updated.State.NextRunAt == nil {
		t.Fatal("expected next run to be scheduled")
	}
	delta := updated.State.NextRunAt.Sub(time.Now().UTC())
	if delta < 500*time.Millisecond || delta > 2*time.Second {
		t.Fatalf("expected next run near one interval from now, got %s", delta)
	}
}

func TestServiceStopDrainsWithoutHanging(t *testing.T) {
	store := newCronMemoryStore()
	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		job := Job{
			ID:      "job-stop-" + string(rune('a'+i)),
			Name:    "stop",
			Enabled: true,
			Schedule: JobSchedule{
				Kind:  ScheduleEvery,
				Every: 1000,
			},
			State: JobState{
				NextRunAt: timePtr(now.Add(-time.Second)),
			},
		}
		if err := store.putJob(job); err != nil {
			t.Fatal(err)
		}
	}

	service := NewService(store, func(ctx context.Context, job Job) (string, error) {
		time.Sleep(40 * time.Millisecond)
		return "ok", nil
	}, &telemetry.Metrics{}, Options{
		Enabled:       true,
		TickInterval:  10 * time.Millisecond,
		MaxConcurrent: 2,
		MaxQueue:      4,
	})
	service.Start()

	done := make(chan struct{})
	go func() {
		service.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stop should return without hanging")
	}
}

func TestServiceRunRecordsIncludeStatusAndTimingFields(t *testing.T) {
	store := newCronMemoryStore()
	now := time.Now().UTC()
	job := Job{
		ID:      "job-record",
		Name:    "record",
		Enabled: true,
		Schedule: JobSchedule{
			Kind:  ScheduleEvery,
			Every: 1000,
		},
		State: JobState{
			NextRunAt: timePtr(now.Add(-time.Second)),
		},
	}
	if err := store.putJob(job); err != nil {
		t.Fatal(err)
	}

	service := NewService(store, func(ctx context.Context, job Job) (string, error) {
		time.Sleep(10 * time.Millisecond)
		return "done", nil
	}, &telemetry.Metrics{}, Options{
		Enabled:       true,
		TickInterval:  10 * time.Millisecond,
		MaxConcurrent: 1,
		MaxQueue:      2,
	})
	service.Start()
	defer service.Stop()

	waitFor(t, time.Second, func() bool { return len(store.runIDs()) > 0 })
	waitFor(t, time.Second, func() bool {
		payload, ok := store.latestRunPayload("job-record")
		if !ok {
			return false
		}
		status, _ := payload["status"].(string)
		return status == "ok"
	})

	payload, ok := store.latestRunPayload("job-record")
	if !ok {
		t.Fatal("expected run payload")
	}
	if payload["job_id"] != "job-record" {
		t.Fatalf("unexpected job_id: %#v", payload["job_id"])
	}
	if payload["run_at"] == nil {
		t.Fatal("expected legacy run_at key")
	}
	if payload["scheduled_at"] == nil {
		t.Fatal("expected scheduled_at key")
	}
	if payload["started_at"] == nil {
		t.Fatal("expected started_at key")
	}
	if payload["finished_at"] == nil {
		t.Fatal("expected finished_at key")
	}
}

func timePtr(v time.Time) *time.Time {
	copy := v
	return &copy
}

func waitFor(t *testing.T, timeout time.Duration, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

type cronMemoryStore struct {
	mu   sync.Mutex
	jobs map[string][]byte
	runs map[string][]byte
}

func newCronMemoryStore() *cronMemoryStore {
	return &cronMemoryStore{
		jobs: map[string][]byte{},
		runs: map[string][]byte{},
	}
}

func (s *cronMemoryStore) PutJob(_ context.Context, job []byte, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[id] = append([]byte(nil), job...)
	return nil
}

func (s *cronMemoryStore) DeleteJob(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.jobs, id)
	return nil
}

func (s *cronMemoryStore) ListJobs(_ context.Context) (map[string][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string][]byte, len(s.jobs))
	for id, bytes := range s.jobs {
		out[id] = append([]byte(nil), bytes...)
	}
	return out, nil
}

func (s *cronMemoryStore) RecordJobRun(_ context.Context, runID string, payload []byte) error {
	if runID == "" {
		return errors.New("run id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[runID] = append([]byte(nil), payload...)
	return nil
}

func (s *cronMemoryStore) putJob(job Job) error {
	bytes, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return s.PutJob(context.Background(), bytes, job.ID)
}

func (s *cronMemoryStore) runIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.runs))
	for id := range s.runs {
		out = append(out, id)
	}
	return out
}

func (s *cronMemoryStore) latestRunPayload(jobID string) (map[string]any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var best map[string]any
	var bestTime time.Time
	for _, payloadBytes := range s.runs {
		payload := map[string]any{}
		if json.Unmarshal(payloadBytes, &payload) != nil {
			continue
		}
		if payload["job_id"] != jobID {
			continue
		}
		runAt, ok := parseMapTime(payload["run_at"])
		if !ok {
			continue
		}
		if best == nil || runAt.After(bestTime) {
			best = payload
			bestTime = runAt
		}
	}
	if best != nil {
		return best, true
	}
	return nil, false
}

func parseMapTime(value any) (time.Time, bool) {
	asString, ok := value.(string)
	if !ok {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, asString)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}
