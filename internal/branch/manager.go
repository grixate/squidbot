package branch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	kvNamespaceRuns = "runtime_branches"
)

var (
	ErrDisabled  = errors.New("branches are disabled")
	ErrQueueFull = errors.New("branch queue is full")
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

func (s Status) Terminal() bool {
	return s == StatusSucceeded || s == StatusFailed
}

type BranchRun struct {
	ID          string    `json:"id"`
	SessionID   string    `json:"session_id"`
	Description string    `json:"description"`
	Prompt      string    `json:"prompt"`
	Status      Status    `json:"status"`
	Conclusion  string    `json:"conclusion,omitempty"`
	Error       string    `json:"error,omitempty"`
	Model       string    `json:"model,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

type Request struct {
	ID          string
	SessionID   string
	Description string
	Prompt      string
}

type Store interface {
	PutKV(ctx context.Context, namespace, key string, value []byte) error
	GetKV(ctx context.Context, namespace, key string) ([]byte, error)
	ListKV(ctx context.Context, namespace, prefix string, limit int) (map[string][]byte, error)
}

type ExecuteFunc func(ctx context.Context, run BranchRun) (conclusion string, model string, err error)

type Options struct {
	Enabled        bool
	MaxConcurrent  int
	MaxQueue       int
	DefaultTimeout time.Duration
	NextID         func() string
	Clock          func() time.Time
}

type Manager struct {
	opts    Options
	store   Store
	exec    ExecuteFunc
	queue   chan string
	stop    chan struct{}
	wg      sync.WaitGroup
	startMu sync.Mutex
	started bool
}

func NewManager(opts Options, store Store, exec ExecuteFunc) *Manager {
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = 3
	}
	if opts.MaxQueue <= 0 {
		opts.MaxQueue = 64
	}
	if opts.DefaultTimeout <= 0 {
		opts.DefaultTimeout = 90 * time.Second
	}
	if opts.NextID == nil {
		opts.NextID = func() string { return fmt.Sprintf("branch-%d", time.Now().UTC().UnixNano()) }
	}
	if opts.Clock == nil {
		opts.Clock = func() time.Time { return time.Now().UTC() }
	}
	return &Manager{
		opts:  opts,
		store: store,
		exec:  exec,
		queue: make(chan string, opts.MaxQueue),
		stop:  make(chan struct{}),
	}
}

func (m *Manager) Start() error {
	if m == nil || !m.opts.Enabled {
		return nil
	}
	m.startMu.Lock()
	defer m.startMu.Unlock()
	if m.started {
		return nil
	}
	if m.exec == nil || m.store == nil {
		return fmt.Errorf("branch manager is not configured")
	}
	for i := 0; i < m.opts.MaxConcurrent; i++ {
		m.wg.Add(1)
		go m.worker()
	}
	m.started = true
	return nil
}

func (m *Manager) Stop() {
	if m == nil {
		return
	}
	m.startMu.Lock()
	if !m.started {
		m.startMu.Unlock()
		return
	}
	close(m.stop)
	m.started = false
	m.startMu.Unlock()
	m.wg.Wait()
}

func (m *Manager) Enqueue(ctx context.Context, req Request) (BranchRun, error) {
	if m == nil || !m.opts.Enabled {
		return BranchRun{}, ErrDisabled
	}
	if m.store == nil || m.exec == nil {
		return BranchRun{}, fmt.Errorf("branch manager is not configured")
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return BranchRun{}, fmt.Errorf("prompt is required")
	}
	runID := strings.TrimSpace(req.ID)
	if runID == "" {
		runID = m.opts.NextID()
	}
	run := BranchRun{
		ID:          runID,
		SessionID:   strings.TrimSpace(req.SessionID),
		Description: strings.TrimSpace(req.Description),
		Prompt:      prompt,
		Status:      StatusQueued,
		CreatedAt:   m.opts.Clock().UTC(),
	}
	if err := m.putRun(ctx, run); err != nil {
		return BranchRun{}, err
	}
	select {
	case m.queue <- run.ID:
		return run, nil
	default:
		run.Status = StatusFailed
		run.Error = ErrQueueFull.Error()
		run.CompletedAt = m.opts.Clock().UTC()
		_ = m.putRun(ctx, run)
		return BranchRun{}, ErrQueueFull
	}
}

func (m *Manager) Status(ctx context.Context, runID string) (BranchRun, error) {
	return m.getRun(ctx, runID)
}

func (m *Manager) Wait(ctx context.Context, runID string, timeout time.Duration) (BranchRun, error) {
	waitCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()

	for {
		run, err := m.getRun(waitCtx, runID)
		if err != nil {
			return BranchRun{}, err
		}
		if run.Status.Terminal() {
			return run, nil
		}
		select {
		case <-waitCtx.Done():
			return run, waitCtx.Err()
		case <-ticker.C:
		}
	}
}

func (m *Manager) List(ctx context.Context, sessionID string, limit int) ([]BranchRun, error) {
	if m == nil || m.store == nil {
		return nil, fmt.Errorf("branch manager is not configured")
	}
	if limit <= 0 {
		limit = 100
	}
	raw, err := m.store.ListKV(ctx, kvNamespaceRuns, "", limit*2)
	if err != nil {
		return nil, err
	}
	out := make([]BranchRun, 0, len(raw))
	sessionID = strings.TrimSpace(sessionID)
	for _, value := range raw {
		var run BranchRun
		if err := json.Unmarshal(value, &run); err != nil {
			continue
		}
		if sessionID != "" && strings.TrimSpace(run.SessionID) != sessionID {
			continue
		}
		out = append(out, run)
	}
	sortRuns(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Manager) worker() {
	defer m.wg.Done()
	for {
		select {
		case <-m.stop:
			return
		case runID := <-m.queue:
			_ = m.runOnce(runID)
		}
	}
}

func (m *Manager) runOnce(runID string) error {
	ctx := context.Background()
	run, err := m.getRun(ctx, runID)
	if err != nil {
		return err
	}
	now := m.opts.Clock().UTC()
	run.Status = StatusRunning
	run.StartedAt = now
	_ = m.putRun(ctx, run)

	runCtx, cancel := context.WithTimeout(context.Background(), m.opts.DefaultTimeout)
	defer cancel()

	conclusion, model, execErr := m.exec(runCtx, run)
	end := m.opts.Clock().UTC()
	run.Model = strings.TrimSpace(model)
	run.Conclusion = strings.TrimSpace(conclusion)
	run.CompletedAt = end
	if execErr != nil {
		run.Status = StatusFailed
		run.Error = execErr.Error()
	} else {
		run.Status = StatusSucceeded
	}
	return m.putRun(ctx, run)
}

func (m *Manager) getRun(ctx context.Context, runID string) (BranchRun, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return BranchRun{}, fmt.Errorf("branch id is required")
	}
	raw, err := m.store.GetKV(ctx, kvNamespaceRuns, runID)
	if err != nil {
		return BranchRun{}, err
	}
	var run BranchRun
	if err := json.Unmarshal(raw, &run); err != nil {
		return BranchRun{}, err
	}
	return run, nil
}

func (m *Manager) putRun(ctx context.Context, run BranchRun) error {
	raw, err := json.Marshal(run)
	if err != nil {
		return err
	}
	return m.store.PutKV(ctx, kvNamespaceRuns, strings.TrimSpace(run.ID), raw)
}

func sortRuns(runs []BranchRun) {
	for i := 0; i < len(runs); i++ {
		for j := i + 1; j < len(runs); j++ {
			if runs[j].CreatedAt.After(runs[i].CreatedAt) {
				runs[i], runs[j] = runs[j], runs[i]
			}
		}
	}
}
