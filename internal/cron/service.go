package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	gocron "github.com/robfig/cron/v3"

	"github.com/grixate/squidbot/internal/telemetry"
)

type SchedulerStore interface {
	PutJob(ctx context.Context, job []byte, id string) error
	DeleteJob(ctx context.Context, id string) error
	ListJobs(ctx context.Context) (map[string][]byte, error)
	RecordJobRun(ctx context.Context, runID string, payload []byte) error
}

type Handler func(ctx context.Context, job Job) (string, error)

type Service struct {
	store   SchedulerStore
	handler Handler
	metrics *telemetry.Metrics
	mu      sync.Mutex
	running bool
	stop    chan struct{}
	wg      sync.WaitGroup
}

func NewService(store SchedulerStore, handler Handler, metrics *telemetry.Metrics) *Service {
	if metrics == nil {
		metrics = &telemetry.Metrics{}
	}
	return &Service{store: store, handler: handler, metrics: metrics, stop: make(chan struct{})}
}

func (s *Service) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	s.wg.Add(1)
	go s.loop()
}

func (s *Service) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()
	close(s.stop)
	s.wg.Wait()
}

func (s *Service) loop() {
	defer s.wg.Done()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.tick(context.Background())
		}
	}
}

func (s *Service) tick(ctx context.Context) {
	jobs, err := s.List(ctx, true)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	for _, job := range jobs {
		if !job.Enabled || job.State.NextRunAt == nil {
			continue
		}
		if now.Before(*job.State.NextRunAt) {
			continue
		}
		s.execute(ctx, job)
	}
}

func (s *Service) execute(ctx context.Context, job Job) {
	s.metrics.CronExecutions.Add(1)
	start := time.Now().UTC()
	result, err := "", error(nil)
	if s.handler != nil {
		result, err = s.handler(ctx, job)
	}
	job.State.LastRunAt = &start
	job.UpdatedAt = time.Now().UTC()
	if err != nil {
		job.State.LastStatus = "error"
		job.State.LastError = err.Error()
	} else {
		job.State.LastStatus = "ok"
		job.State.LastError = ""
	}
	next := computeNextRun(job.Schedule, time.Now().UTC())
	job.State.NextRunAt = next
	if job.Schedule.Kind == ScheduleAt {
		job.Enabled = false
	}
	_ = s.Put(ctx, job)
	_ = s.recordRun(ctx, job, result, err)
}

func (s *Service) recordRun(ctx context.Context, job Job, result string, err error) error {
	payload := map[string]any{
		"job_id": job.ID,
		"run_at": time.Now().UTC(),
		"result": result,
	}
	if err != nil {
		payload["error"] = err.Error()
	}
	data, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return marshalErr
	}
	return s.store.RecordJobRun(ctx, fmt.Sprintf("%s:%d", job.ID, time.Now().UnixNano()), data)
}

func (s *Service) Put(ctx context.Context, job Job) error {
	job.UpdatedAt = time.Now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = job.UpdatedAt
	}
	if job.Version == 0 {
		job.Version = 1
	}
	if job.State.NextRunAt == nil && job.Enabled {
		job.State.NextRunAt = computeNextRun(job.Schedule, time.Now().UTC())
	}
	bytes, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return s.store.PutJob(ctx, bytes, job.ID)
}

func (s *Service) Remove(ctx context.Context, id string) error {
	return s.store.DeleteJob(ctx, id)
}

func (s *Service) Get(ctx context.Context, id string) (*Job, error) {
	jobs, err := s.List(ctx, true)
	if err != nil {
		return nil, err
	}
	for _, job := range jobs {
		if job.ID == id {
			copy := job
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("job not found")
}

func (s *Service) List(ctx context.Context, includeDisabled bool) ([]Job, error) {
	raw, err := s.store.ListJobs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Job, 0, len(raw))
	for _, bytes := range raw {
		var job Job
		if json.Unmarshal(bytes, &job) != nil {
			continue
		}
		if !includeDisabled && !job.Enabled {
			continue
		}
		out = append(out, job)
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := out[i].State.NextRunAt, out[j].State.NextRunAt
		if left == nil && right == nil {
			return out[i].Name < out[j].Name
		}
		if left == nil {
			return false
		}
		if right == nil {
			return true
		}
		return left.Before(*right)
	})
	return out, nil
}

func (s *Service) Enable(ctx context.Context, id string, enabled bool) error {
	job, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	job.Enabled = enabled
	if enabled {
		job.State.NextRunAt = computeNextRun(job.Schedule, time.Now().UTC())
	} else {
		job.State.NextRunAt = nil
	}
	return s.Put(ctx, *job)
}

func (s *Service) RunNow(ctx context.Context, id string, force bool) error {
	job, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if !force && !job.Enabled {
		return fmt.Errorf("job is disabled")
	}
	s.execute(ctx, *job)
	return nil
}

func computeNextRun(schedule JobSchedule, now time.Time) *time.Time {
	switch schedule.Kind {
	case ScheduleAt:
		if schedule.At == nil {
			return nil
		}
		if schedule.At.After(now) {
			t := schedule.At.UTC()
			return &t
		}
		return nil
	case ScheduleEvery:
		if schedule.Every <= 0 {
			return nil
		}
		next := now.Add(time.Duration(schedule.Every) * time.Millisecond)
		next = next.UTC()
		return &next
	case ScheduleCron:
		expr := strings.TrimSpace(schedule.Expr)
		if expr == "" {
			return nil
		}
		parser := gocron.NewParser(gocron.Minute | gocron.Hour | gocron.Dom | gocron.Month | gocron.Dow)
		sched, err := parser.Parse(expr)
		if err != nil {
			return nil
		}
		next := sched.Next(now)
		next = next.UTC()
		return &next
	default:
		return nil
	}
}
