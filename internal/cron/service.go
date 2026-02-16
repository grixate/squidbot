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

type Options struct {
	Enabled       bool
	TickInterval  time.Duration
	MaxConcurrent int
	MaxQueue      int
	Clock         func() time.Time
}

type dispatchItem struct {
	runID       string
	job         Job
	scheduledAt time.Time
	queuedAt    time.Time
	ready       chan struct{}
	skip        bool
	skipErr     error
}

type Service struct {
	store   SchedulerStore
	handler Handler
	metrics *telemetry.Metrics
	opts    Options
	mu      sync.Mutex
	running bool
	stop    chan struct{}
	loopWG  sync.WaitGroup
	workWG  sync.WaitGroup
	queue   chan *dispatchItem
}

func NewService(store SchedulerStore, handler Handler, metrics *telemetry.Metrics, options ...Options) *Service {
	if metrics == nil {
		metrics = &telemetry.Metrics{}
	}
	opts := Options{}
	if len(options) > 0 {
		opts = options[0]
	} else {
		opts.Enabled = true
	}
	if opts.TickInterval <= 0 {
		opts.TickInterval = time.Second
	}
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = 4
	}
	if opts.MaxQueue <= 0 {
		opts.MaxQueue = 128
	}
	if opts.Clock == nil {
		opts.Clock = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		store:   store,
		handler: handler,
		metrics: metrics,
		opts:    opts,
		stop:    make(chan struct{}),
		queue:   make(chan *dispatchItem, opts.MaxQueue),
	}
}

func (s *Service) Start() {
	if !s.opts.Enabled {
		return
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	s.loopWG.Add(1)
	go s.loop()
	for i := 0; i < s.opts.MaxConcurrent; i++ {
		s.workWG.Add(1)
		go s.worker()
	}
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
	s.loopWG.Wait()
	close(s.queue)
	s.workWG.Wait()
}

func (s *Service) loop() {
	defer s.loopWG.Done()
	s.tick(context.Background())
	ticker := time.NewTicker(s.opts.TickInterval)
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
	now := s.nowUTC()
	for _, job := range jobs {
		if !job.Enabled || job.State.NextRunAt == nil {
			continue
		}
		if now.Before(*job.State.NextRunAt) {
			continue
		}
		s.dispatch(ctx, job, now)
	}
}

func (s *Service) worker() {
	defer s.workWG.Done()
	for item := range s.queue {
		s.updateQueueDepth()
		<-item.ready
		if item.skip {
			_ = s.recordRun(context.Background(), runRecord{
				RunID:       item.runID,
				JobID:       item.job.ID,
				Status:      "error",
				ScheduledAt: item.scheduledAt,
				RunAt:       s.nowUTC(),
				Error:       item.skipErr,
			})
			s.metrics.CronFailed.Add(1)
			continue
		}
		s.executeDispatched(context.Background(), item)
	}
}

func (s *Service) dispatch(ctx context.Context, job Job, now time.Time) {
	scheduledAt := now
	if job.State.NextRunAt != nil {
		scheduledAt = job.State.NextRunAt.UTC()
	}
	item := &dispatchItem{
		runID:       fmt.Sprintf("%s:%d", job.ID, now.UnixNano()),
		job:         job,
		scheduledAt: scheduledAt,
		queuedAt:    now.UTC(),
		ready:       make(chan struct{}),
	}

	select {
	case s.queue <- item:
		s.metrics.CronQueued.Add(1)
		s.updateQueueDepth()
	default:
		s.metrics.CronQueueFull.Add(1)
		return
	}

	queuedAt := item.queuedAt
	job.State.LastRunAt = &queuedAt
	job.State.NextRunAt = computeNextRun(job.Schedule, queuedAt)
	if job.Schedule.Kind == ScheduleAt {
		job.Enabled = false
	}
	if err := s.Put(ctx, job); err != nil {
		item.skip = true
		item.skipErr = err
		close(item.ready)
		return
	}
	item.job = job

	_ = s.recordRun(ctx, runRecord{
		RunID:       item.runID,
		JobID:       item.job.ID,
		Status:      "queued",
		ScheduledAt: item.scheduledAt,
		RunAt:       queuedAt,
	})
	close(item.ready)
}

func (s *Service) executeDispatched(ctx context.Context, item *dispatchItem) {
	s.metrics.CronExecutions.Add(1)
	s.metrics.CronRunning.Add(1)
	defer s.metrics.CronRunning.Add(-1)

	start := s.nowUTC()
	dispatchLag := int64(start.Sub(item.scheduledAt).Milliseconds())
	if dispatchLag < 0 {
		dispatchLag = 0
	}
	s.metrics.CronDispatchLagMS.Add(uint64(dispatchLag))
	_ = s.recordRun(ctx, runRecord{
		RunID:         item.runID,
		JobID:         item.job.ID,
		Status:        "running",
		ScheduledAt:   item.scheduledAt,
		StartedAt:     &start,
		RunAt:         start,
		DispatchLagMS: dispatchLag,
	})

	result, err := "", error(nil)
	if s.handler != nil {
		result, err = s.handler(ctx, item.job)
	}
	finishedAt := s.nowUTC()
	duration := int64(finishedAt.Sub(start).Milliseconds())
	if duration < 0 {
		duration = 0
	}
	s.metrics.CronRunDurationMS.Add(uint64(duration))

	job := item.job
	job.UpdatedAt = finishedAt
	if err != nil {
		job.State.LastStatus = "error"
		job.State.LastError = err.Error()
		s.metrics.CronFailed.Add(1)
	} else {
		job.State.LastStatus = "ok"
		job.State.LastError = ""
		s.metrics.CronSucceeded.Add(1)
	}
	_ = s.Put(ctx, job)
	_ = s.recordRun(ctx, runRecord{
		RunID:         item.runID,
		JobID:         item.job.ID,
		Status:        job.State.LastStatus,
		ScheduledAt:   item.scheduledAt,
		StartedAt:     &start,
		FinishedAt:    &finishedAt,
		RunAt:         finishedAt,
		Result:        result,
		Error:         err,
		DispatchLagMS: dispatchLag,
		DurationMS:    duration,
	})
}

type runRecord struct {
	RunID         string
	JobID         string
	Status        string
	ScheduledAt   time.Time
	StartedAt     *time.Time
	FinishedAt    *time.Time
	RunAt         time.Time
	Result        string
	Error         error
	DispatchLagMS int64
	DurationMS    int64
}

func (s *Service) recordRun(ctx context.Context, record runRecord) error {
	if record.RunAt.IsZero() {
		record.RunAt = s.nowUTC()
	}
	payload := map[string]any{
		"job_id":       record.JobID,
		"run_at":       record.RunAt.UTC(),
		"status":       strings.TrimSpace(record.Status),
		"scheduled_at": record.ScheduledAt.UTC(),
		"result":       record.Result,
	}
	if record.StartedAt != nil {
		payload["started_at"] = record.StartedAt.UTC()
	}
	if record.FinishedAt != nil {
		payload["finished_at"] = record.FinishedAt.UTC()
	}
	if record.DispatchLagMS > 0 {
		payload["dispatch_lag_ms"] = record.DispatchLagMS
	}
	if record.DurationMS > 0 {
		payload["duration_ms"] = record.DurationMS
	}
	if record.Error != nil {
		payload["error"] = record.Error.Error()
	}
	data, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return marshalErr
	}
	return s.store.RecordJobRun(ctx, record.RunID, data)
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
	now := s.nowUTC()
	scheduledAt := now
	job.State.LastRunAt = &now
	if job.Enabled {
		job.State.NextRunAt = computeNextRun(job.Schedule, now)
	} else {
		job.State.NextRunAt = nil
	}
	if job.Schedule.Kind == ScheduleAt {
		job.Enabled = false
	}
	if err := s.Put(ctx, *job); err != nil {
		return err
	}
	item := &dispatchItem{
		runID:       fmt.Sprintf("%s:%d", job.ID, now.UnixNano()),
		job:         *job,
		scheduledAt: scheduledAt,
		queuedAt:    now,
	}
	_ = s.recordRun(ctx, runRecord{
		RunID:       item.runID,
		JobID:       item.job.ID,
		Status:      "queued",
		ScheduledAt: item.scheduledAt,
		RunAt:       item.queuedAt,
	})
	s.executeDispatched(ctx, item)
	return nil
}

func (s *Service) nowUTC() time.Time {
	if s.opts.Clock == nil {
		return time.Now().UTC()
	}
	return s.opts.Clock().UTC()
}

func (s *Service) updateQueueDepth() {
	s.metrics.CronQueueDepth.Store(uint64(len(s.queue)))
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
