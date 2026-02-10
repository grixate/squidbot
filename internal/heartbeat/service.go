package heartbeat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/grixate/squidbot/internal/telemetry"
)

const Prompt = `Read HEARTBEAT.md in your workspace (if it exists).
Follow any instructions or tasks listed there.
If nothing needs attention, reply with just: HEARTBEAT_OK`

type Handler func(ctx context.Context, prompt string) (string, error)

type RunRecord struct {
	TriggeredBy string
	Status      string
	Error       string
	Response    string
	StartedAt   time.Time
	FinishedAt  time.Time
}

type RunObserver func(record RunRecord)

type Service struct {
	workspace string
	handler   Handler
	metrics   *telemetry.Metrics
	mu        sync.Mutex
	running   bool
	stop      chan struct{}
	reset     chan time.Duration
	interval  time.Duration
	nextRunAt time.Time
	lastRun   RunRecord
	hasLast   bool
	observer  RunObserver
	wg        sync.WaitGroup
}

func NewService(workspace string, interval time.Duration, handler Handler, metrics *telemetry.Metrics) *Service {
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	if metrics == nil {
		metrics = &telemetry.Metrics{}
	}
	return &Service{
		workspace: workspace,
		handler:   handler,
		interval:  interval,
		metrics:   metrics,
		stop:      make(chan struct{}),
		reset:     make(chan time.Duration, 1),
	}
}

func (s *Service) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.nextRunAt = time.Now().UTC().Add(s.interval)
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
	s.nextRunAt = time.Time{}
	s.mu.Unlock()
	close(s.stop)
	s.wg.Wait()
}

func (s *Service) loop() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.Interval())
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case interval := <-s.reset:
			if interval <= 0 {
				continue
			}
			ticker.Reset(interval)
			s.mu.Lock()
			s.nextRunAt = time.Now().UTC().Add(interval)
			s.mu.Unlock()
		case <-ticker.C:
			interval := s.Interval()
			s.mu.Lock()
			s.nextRunAt = time.Now().UTC().Add(interval)
			s.mu.Unlock()
			s.tick(context.Background())
		}
	}
}

func (s *Service) TriggerNow(ctx context.Context) (string, error) {
	if s.handler == nil {
		return "", nil
	}
	return s.execute(ctx, "manual")
}

func (s *Service) Interval() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.interval
}

func (s *Service) SetInterval(interval time.Duration) {
	if interval <= 0 {
		return
	}
	s.mu.Lock()
	s.interval = interval
	isRunning := s.running
	if isRunning {
		s.nextRunAt = time.Now().UTC().Add(interval)
	}
	s.mu.Unlock()
	if !isRunning {
		return
	}
	select {
	case s.reset <- interval:
	default:
	}
}

func (s *Service) NextRunAt() (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.nextRunAt.IsZero() {
		return time.Time{}, false
	}
	return s.nextRunAt, true
}

func (s *Service) LastRun() (RunRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasLast {
		return RunRecord{}, false
	}
	return s.lastRun, true
}

func (s *Service) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *Service) SetRunObserver(observer RunObserver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observer = observer
}

func (s *Service) tick(ctx context.Context) {
	heartbeatPath := filepath.Join(s.workspace, "HEARTBEAT.md")
	bytes, err := os.ReadFile(heartbeatPath)
	if err != nil {
		return
	}
	if isEmpty(string(bytes)) {
		return
	}
	if s.handler == nil {
		return
	}
	_, _ = s.execute(ctx, "scheduled")
}

func (s *Service) execute(ctx context.Context, triggeredBy string) (string, error) {
	started := time.Now().UTC()
	s.metrics.HeartbeatExecutions.Add(1)
	result, err := s.handler(ctx, Prompt)
	finished := time.Now().UTC()

	record := RunRecord{
		TriggeredBy: triggeredBy,
		Status:      "ok",
		Error:       "",
		Response:    strings.TrimSpace(result),
		StartedAt:   started,
		FinishedAt:  finished,
	}
	if err != nil {
		record.Status = "error"
		record.Error = err.Error()
	}

	s.mu.Lock()
	s.lastRun = record
	s.hasLast = true
	observer := s.observer
	s.mu.Unlock()

	if observer != nil {
		observer(record)
	}
	return result, err
}

func isEmpty(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "<!--") {
			continue
		}
		if trimmed == "- [ ]" || trimmed == "* [ ]" || trimmed == "- [x]" || trimmed == "* [x]" {
			continue
		}
		return false
	}
	return true
}
