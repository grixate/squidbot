package subagent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/grixate/squidbot/internal/telemetry"
)

var (
	ErrDisabled      = errors.New("subagents are disabled")
	ErrQueueFull     = errors.New("subagent queue is full")
	ErrDepthExceeded = errors.New("subagent depth exceeded")
)

type Store interface {
	PutSubagentRun(ctx context.Context, run Run) error
	GetSubagentRun(ctx context.Context, id string) (Run, error)
	ListSubagentRunsBySession(ctx context.Context, sessionID string, limit int) ([]Run, error)
	ListSubagentRunsByStatus(ctx context.Context, status Status, limit int) ([]Run, error)
	AppendSubagentEvent(ctx context.Context, event Event) error
	PutKV(ctx context.Context, namespace, key string, value []byte) error
	GetKV(ctx context.Context, namespace, key string) ([]byte, error)
}

type Executor func(ctx context.Context, run Run) (Result, error)

type NotifyFunc func(run Run)

type IDFunc func() string

type ClockFunc func() time.Time

type Options struct {
	Enabled          bool
	MaxConcurrent    int
	MaxQueue         int
	DefaultTimeout   time.Duration
	MaxAttempts      int
	RetryBackoff     time.Duration
	MaxDepth         int
	NotifyOnComplete bool
	NextID           IDFunc
	Clock            ClockFunc
}

type Manager struct {
	opts    Options
	store   Store
	exec    Executor
	notify  NotifyFunc
	metrics *telemetry.Metrics

	queue chan string
	stop  chan struct{}
	wg    sync.WaitGroup

	startOnce sync.Once
	stopOnce  sync.Once

	cancelMu sync.Mutex
	cancels  map[string]context.CancelFunc
}

func NewManager(opts Options, store Store, exec Executor, notify NotifyFunc, metrics *telemetry.Metrics) *Manager {
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = 4
	}
	if opts.MaxQueue <= 0 {
		opts.MaxQueue = 64
	}
	if opts.DefaultTimeout <= 0 {
		opts.DefaultTimeout = 5 * time.Minute
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 2
	}
	if opts.RetryBackoff < 0 {
		opts.RetryBackoff = 0
	}
	if opts.MaxDepth < 0 {
		opts.MaxDepth = 0
	}
	if opts.NextID == nil {
		opts.NextID = func() string {
			return fmt.Sprintf("subagent-%d", time.Now().UTC().UnixNano())
		}
	}
	if opts.Clock == nil {
		opts.Clock = func() time.Time { return time.Now().UTC() }
	}
	return &Manager{
		opts:    opts,
		store:   store,
		exec:    exec,
		notify:  notify,
		metrics: metrics,
		queue:   make(chan string, opts.MaxQueue),
		stop:    make(chan struct{}),
		cancels: map[string]context.CancelFunc{},
	}
}

func (m *Manager) Start(ctx context.Context) error {
	if m == nil || !m.opts.Enabled {
		return nil
	}
	var startErr error
	m.startOnce.Do(func() {
		for i := 0; i < m.opts.MaxConcurrent; i++ {
			m.wg.Add(1)
			go m.worker()
		}
		startErr = m.Recover(ctx)
	})
	return startErr
}

func (m *Manager) Stop() {
	if m == nil {
		return
	}
	m.stopOnce.Do(func() {
		close(m.stop)
		m.wg.Wait()
	})
}

func (m *Manager) Enqueue(ctx context.Context, req Request) (Run, error) {
	if m == nil || !m.opts.Enabled {
		return Run{}, ErrDisabled
	}
	if m.store == nil || m.exec == nil {
		return Run{}, fmt.Errorf("subagent manager not configured")
	}
	task := strings.TrimSpace(req.Task)
	if task == "" {
		return Run{}, fmt.Errorf("task is required")
	}
	if req.Depth > m.opts.MaxDepth {
		return Run{}, ErrDepthExceeded
	}
	timeoutSec := req.TimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = int(m.opts.DefaultTimeout.Seconds())
	}
	maxAttempts := req.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = m.opts.MaxAttempts
	}
	now := m.opts.Clock().UTC()
	runID := strings.TrimSpace(req.ID)
	if runID == "" {
		runID = m.opts.NextID()
	}
	run := Run{
		ID:               runID,
		SessionID:        strings.TrimSpace(req.SessionID),
		Channel:          strings.TrimSpace(req.Channel),
		ChatID:           strings.TrimSpace(req.ChatID),
		SenderID:         strings.TrimSpace(req.SenderID),
		Label:            strings.TrimSpace(req.Label),
		Task:             task,
		Status:           StatusQueued,
		CreatedAt:        now,
		TimeoutSec:       timeoutSec,
		MaxAttempts:      maxAttempts,
		Depth:            req.Depth,
		NotifyOnComplete: req.NotifyOnComplete,
		ArtifactDir:      strings.TrimSpace(req.ArtifactDir),
		Context:          req.Context,
	}
	if err := m.store.PutSubagentRun(ctx, run); err != nil {
		return Run{}, err
	}
	if err := m.recordEvent(ctx, run.ID, StatusQueued, "run queued", 0); err != nil {
		return Run{}, err
	}
	if err := m.enqueueRunID(run.ID); err != nil {
		return Run{}, err
	}
	if m.metrics != nil {
		m.metrics.SubagentQueued.Add(1)
	}
	return run, nil
}

func (m *Manager) Recover(ctx context.Context) error {
	if m == nil || !m.opts.Enabled || m.store == nil {
		return nil
	}
	queued, err := m.store.ListSubagentRunsByStatus(ctx, StatusQueued, 0)
	if err != nil {
		return err
	}
	running, err := m.store.ListSubagentRunsByStatus(ctx, StatusRunning, 0)
	if err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, run := range queued {
		if _, ok := seen[run.ID]; ok {
			continue
		}
		seen[run.ID] = struct{}{}
		if run.Status.Terminal() {
			continue
		}
		if err := m.enqueueRunID(run.ID); err != nil {
			return err
		}
	}
	for _, run := range running {
		if _, ok := seen[run.ID]; ok {
			continue
		}
		run.Status = StatusQueued
		run.Error = "recovered after restart"
		if err := m.store.PutSubagentRun(ctx, run); err != nil {
			return err
		}
		if err := m.recordEvent(ctx, run.ID, StatusQueued, "run recovered and re-queued", run.Attempt); err != nil {
			return err
		}
		if err := m.enqueueRunID(run.ID); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) Wait(ctx context.Context, runIDs []string, timeout time.Duration) ([]Run, error) {
	if m == nil {
		return nil, fmt.Errorf("subagent manager not configured")
	}
	ids := make([]string, 0, len(runIDs))
	seen := map[string]struct{}{}
	for _, id := range runIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		ids = append(ids, trimmed)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("run_ids is required")
	}
	waitCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		runs, err := m.loadRuns(waitCtx, ids)
		if err != nil {
			return nil, err
		}
		allTerminal := true
		for _, run := range runs {
			if !run.Status.Terminal() {
				allTerminal = false
				break
			}
		}
		if allTerminal {
			return runs, nil
		}
		select {
		case <-waitCtx.Done():
			return runs, waitCtx.Err()
		case <-ticker.C:
		}
	}
}

func (m *Manager) Status(ctx context.Context, runID string) (Run, error) {
	if m == nil || m.store == nil {
		return Run{}, fmt.Errorf("subagent manager not configured")
	}
	return m.store.GetSubagentRun(ctx, strings.TrimSpace(runID))
}

func (m *Manager) Result(ctx context.Context, runID string) (Run, error) {
	run, err := m.Status(ctx, runID)
	if err != nil {
		return Run{}, err
	}
	if !run.Status.Terminal() {
		return run, fmt.Errorf("run %s is not complete", run.ID)
	}
	return run, nil
}

func (m *Manager) Cancel(ctx context.Context, runID string) (Run, error) {
	if m == nil || m.store == nil {
		return Run{}, fmt.Errorf("subagent manager not configured")
	}
	run, err := m.store.GetSubagentRun(ctx, strings.TrimSpace(runID))
	if err != nil {
		return Run{}, err
	}
	if run.Status.Terminal() {
		return run, nil
	}
	if err := m.signalCancel(ctx, run.ID); err != nil {
		return Run{}, err
	}
	cancelledAt := m.opts.Clock().UTC()
	run.Status = StatusCancelled
	run.Error = "cancelled"
	run.FinishedAt = &cancelledAt
	if err := m.store.PutSubagentRun(ctx, run); err != nil {
		return Run{}, err
	}
	if err := m.recordEvent(ctx, run.ID, StatusCancelled, "run cancelled", run.Attempt); err != nil {
		return Run{}, err
	}
	if m.metrics != nil {
		m.metrics.SubagentCancelled.Add(1)
	}
	m.cancelMu.Lock()
	cancel := m.cancels[run.ID]
	m.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if run.NotifyOnComplete && m.notify != nil {
		m.notify(run)
	}
	return run, nil
}

func (m *Manager) ListSessionRuns(ctx context.Context, sessionID string, limit int) ([]Run, error) {
	if m == nil || m.store == nil {
		return nil, fmt.Errorf("subagent manager not configured")
	}
	return m.store.ListSubagentRunsBySession(ctx, strings.TrimSpace(sessionID), limit)
}

func (m *Manager) worker() {
	defer m.wg.Done()
	for {
		select {
		case <-m.stop:
			return
		case runID := <-m.queue:
			m.updateQueueDepth()
			m.executeRun(runID)
		}
	}
}

func (m *Manager) executeRun(runID string) {
	ctx := context.Background()
	run, err := m.store.GetSubagentRun(ctx, runID)
	if err != nil {
		return
	}
	if run.Status.Terminal() {
		return
	}
	if m.hasCancelSignal(ctx, run.ID) {
		m.finalizeCancelled(ctx, run, "run cancelled by external signal")
		return
	}
	maxAttempts := run.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = m.opts.MaxAttempts
	}
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	for {
		if m.hasCancelSignal(ctx, run.ID) {
			m.finalizeCancelled(ctx, run, "run cancelled by external signal")
			return
		}
		if run.Attempt >= maxAttempts {
			finishedAt := m.opts.Clock().UTC()
			run.Status = StatusFailed
			run.Error = "attempt budget exhausted"
			run.FinishedAt = &finishedAt
			_ = m.store.PutSubagentRun(ctx, run)
			_ = m.recordEvent(ctx, run.ID, StatusFailed, run.Error, run.Attempt)
			if m.metrics != nil {
				m.metrics.SubagentFailed.Add(1)
			}
			if run.NotifyOnComplete && m.notify != nil {
				m.notify(run)
			}
			return
		}

		run.Attempt++
		startedAt := m.opts.Clock().UTC()
		run.Status = StatusRunning
		run.Error = ""
		if run.StartedAt == nil {
			run.StartedAt = &startedAt
		}
		if err := m.store.PutSubagentRun(ctx, run); err != nil {
			return
		}
		if err := m.recordEvent(ctx, run.ID, StatusRunning, "run started", run.Attempt); err != nil {
			return
		}
		if m.metrics != nil {
			m.metrics.SubagentRunning.Add(1)
		}

		timeout := time.Duration(run.TimeoutSec) * time.Second
		if timeout <= 0 {
			timeout = m.opts.DefaultTimeout
		}
		runCtx, cancel := context.WithTimeout(context.Background(), timeout)
		m.registerCancel(run.ID, cancel)
		go m.watchCancelSignal(runCtx, run.ID, cancel)
		result, runErr := m.exec(runCtx, run)
		cancel()
		m.unregisterCancel(run.ID)

		if runErr == nil {
			finishedAt := m.opts.Clock().UTC()
			run.Status = StatusSucceeded
			run.Error = ""
			run.FinishedAt = &finishedAt
			run.Result = &result
			if err := m.store.PutSubagentRun(ctx, run); err != nil {
				return
			}
			if err := m.recordEvent(ctx, run.ID, StatusSucceeded, "run completed", run.Attempt); err != nil {
				return
			}
			if m.metrics != nil {
				m.metrics.SubagentSucceeded.Add(1)
			}
			if run.NotifyOnComplete && m.notify != nil {
				m.notify(run)
			}
			return
		}

		latest, latestErr := m.store.GetSubagentRun(ctx, run.ID)
		if latestErr == nil && latest.Status == StatusCancelled {
			return
		}

		if errors.Is(runErr, context.Canceled) {
			m.finalizeCancelled(ctx, run, "run cancelled")
			return
		}

		shouldRetry := run.Attempt < maxAttempts
		if shouldRetry {
			run.Status = StatusQueued
			run.Error = runErr.Error()
			if err := m.store.PutSubagentRun(ctx, run); err != nil {
				return
			}
			if err := m.recordEvent(ctx, run.ID, StatusQueued, "retry scheduled: "+runErr.Error(), run.Attempt); err != nil {
				return
			}
			if m.metrics != nil {
				m.metrics.SubagentRetries.Add(1)
			}
			if !m.sleepBackoff() {
				return
			}
			continue
		}

		finishedAt := m.opts.Clock().UTC()
		run.Error = runErr.Error()
		run.FinishedAt = &finishedAt
		if errors.Is(runErr, context.DeadlineExceeded) {
			run.Status = StatusTimedOut
			if m.metrics != nil {
				m.metrics.SubagentTimedOut.Add(1)
			}
		} else {
			run.Status = StatusFailed
			if m.metrics != nil {
				m.metrics.SubagentFailed.Add(1)
			}
		}
		if err := m.store.PutSubagentRun(ctx, run); err != nil {
			return
		}
		if err := m.recordEvent(ctx, run.ID, run.Status, run.Error, run.Attempt); err != nil {
			return
		}
		if run.NotifyOnComplete && m.notify != nil {
			m.notify(run)
		}
		return
	}
}

func (m *Manager) signalCancel(ctx context.Context, runID string) error {
	if m == nil || m.store == nil {
		return nil
	}
	return m.store.PutKV(ctx, CancelSignalNamespace, strings.TrimSpace(runID), []byte(m.opts.Clock().UTC().Format(time.RFC3339Nano)))
}

func (m *Manager) hasCancelSignal(ctx context.Context, runID string) bool {
	if m == nil || m.store == nil {
		return false
	}
	value, err := m.store.GetKV(ctx, CancelSignalNamespace, strings.TrimSpace(runID))
	if err != nil {
		return false
	}
	return len(value) > 0
}

func (m *Manager) watchCancelSignal(ctx context.Context, runID string, cancel context.CancelFunc) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stop:
			cancel()
			return
		case <-ticker.C:
			if m.hasCancelSignal(context.Background(), runID) {
				cancel()
				return
			}
		}
	}
}

func (m *Manager) finalizeCancelled(ctx context.Context, run Run, message string) {
	finishedAt := m.opts.Clock().UTC()
	run.Status = StatusCancelled
	run.Error = strings.TrimSpace(message)
	if run.Error == "" {
		run.Error = "cancelled"
	}
	run.FinishedAt = &finishedAt
	_ = m.store.PutSubagentRun(ctx, run)
	_ = m.recordEvent(ctx, run.ID, StatusCancelled, run.Error, run.Attempt)
	if m.metrics != nil {
		m.metrics.SubagentCancelled.Add(1)
	}
	if run.NotifyOnComplete && m.notify != nil {
		m.notify(run)
	}
}

func (m *Manager) registerCancel(runID string, cancel context.CancelFunc) {
	m.cancelMu.Lock()
	defer m.cancelMu.Unlock()
	m.cancels[runID] = cancel
}

func (m *Manager) unregisterCancel(runID string) {
	m.cancelMu.Lock()
	defer m.cancelMu.Unlock()
	delete(m.cancels, runID)
}

func (m *Manager) sleepBackoff() bool {
	if m.opts.RetryBackoff <= 0 {
		return true
	}
	timer := time.NewTimer(m.opts.RetryBackoff)
	defer timer.Stop()
	select {
	case <-m.stop:
		return false
	case <-timer.C:
		return true
	}
}

func (m *Manager) enqueueRunID(runID string) error {
	select {
	case m.queue <- runID:
		m.updateQueueDepth()
		return nil
	default:
		return ErrQueueFull
	}
}

func (m *Manager) updateQueueDepth() {
	if m.metrics == nil {
		return
	}
	m.metrics.SubagentQueueDepth.Store(uint64(len(m.queue)))
}

func (m *Manager) loadRuns(ctx context.Context, ids []string) ([]Run, error) {
	runs := make([]Run, 0, len(ids))
	for _, id := range ids {
		run, err := m.store.GetSubagentRun(ctx, id)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}

func (m *Manager) recordEvent(ctx context.Context, runID string, status Status, message string, attempt int) error {
	if m.store == nil {
		return nil
	}
	return m.store.AppendSubagentEvent(ctx, Event{
		ID:        m.opts.NextID(),
		RunID:     runID,
		Status:    status,
		Message:   strings.TrimSpace(message),
		Attempt:   attempt,
		CreatedAt: m.opts.Clock().UTC(),
	})
}
