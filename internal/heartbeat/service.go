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

type Service struct {
	workspace string
	handler   Handler
	interval  time.Duration
	metrics   *telemetry.Metrics
	mu        sync.Mutex
	running   bool
	stop      chan struct{}
	wg        sync.WaitGroup
}

func NewService(workspace string, interval time.Duration, handler Handler, metrics *telemetry.Metrics) *Service {
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	if metrics == nil {
		metrics = &telemetry.Metrics{}
	}
	return &Service{workspace: workspace, handler: handler, interval: interval, metrics: metrics, stop: make(chan struct{})}
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
	ticker := time.NewTicker(s.interval)
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

func (s *Service) TriggerNow(ctx context.Context) (string, error) {
	if s.handler == nil {
		return "", nil
	}
	s.metrics.HeartbeatExecutions.Add(1)
	return s.handler(ctx, Prompt)
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
	s.metrics.HeartbeatExecutions.Add(1)
	_, _ = s.handler(ctx, Prompt)
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
