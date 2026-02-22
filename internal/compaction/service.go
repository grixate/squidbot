package compaction

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

const kvNamespaceRuns = "runtime_compaction_runs"

type Turn struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type CompactionRun struct {
	SessionID    string    `json:"session_id"`
	ThresholdHit int       `json:"threshold_hit"`
	Action       string    `json:"action"`
	RemovedTurns int       `json:"removed_turns"`
	Summary      string    `json:"summary,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type KVStore interface {
	PutKV(ctx context.Context, namespace, key string, value []byte) error
	ListKV(ctx context.Context, namespace, prefix string, limit int) (map[string][]byte, error)
}

type Options struct {
	Enabled                bool
	MaxHistoryTurns        int
	KeepMinTurns           int
	BackgroundThresholdPct int
	AggressiveThresholdPct int
	EmergencyThresholdPct  int
	ListTurns              func(ctx context.Context, sessionID string, limit int) ([]Turn, error)
	DeleteTurns            func(ctx context.Context, sessionID string, turnIDs []string) error
	AppendSummaryMarker    func(ctx context.Context, sessionID, summary string) error
	Summarize              func(ctx context.Context, sessionID string, removed []Turn) (string, error)
}

type Service struct {
	store KVStore
	opts  Options
	mu    sync.Mutex
	active map[string]struct{}
}

func NewService(store KVStore, opts Options) *Service {
	if opts.MaxHistoryTurns <= 0 {
		opts.MaxHistoryTurns = 50
	}
	if opts.KeepMinTurns <= 0 {
		opts.KeepMinTurns = 8
	}
	if opts.BackgroundThresholdPct <= 0 {
		opts.BackgroundThresholdPct = 72
	}
	if opts.AggressiveThresholdPct <= 0 {
		opts.AggressiveThresholdPct = 84
	}
	if opts.EmergencyThresholdPct <= 0 {
		opts.EmergencyThresholdPct = 94
	}
	if opts.AggressiveThresholdPct < opts.BackgroundThresholdPct {
		opts.AggressiveThresholdPct = opts.BackgroundThresholdPct
	}
	if opts.EmergencyThresholdPct < opts.AggressiveThresholdPct {
		opts.EmergencyThresholdPct = opts.AggressiveThresholdPct
	}
	return &Service{
		store: store,
		opts:  opts,
		active: map[string]struct{}{},
	}
}

func (s *Service) ObserveTurn(ctx context.Context, sessionID string) {
	if s == nil || !s.opts.Enabled || s.opts.ListTurns == nil || s.opts.DeleteTurns == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	s.mu.Lock()
	if _, exists := s.active[sessionID]; exists {
		s.mu.Unlock()
		return
	}
	s.active[sessionID] = struct{}{}
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.active, sessionID)
			s.mu.Unlock()
		}()
		s.compact(context.Background(), sessionID)
	}()
}

func (s *Service) ListRuns(ctx context.Context, limit int) ([]CompactionRun, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	raw, err := s.store.ListKV(ctx, kvNamespaceRuns, "", limit*2)
	if err != nil {
		return nil, err
	}
	out := make([]CompactionRun, 0, len(raw))
	for _, item := range raw {
		var run CompactionRun
		if err := json.Unmarshal(item, &run); err != nil {
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

func (s *Service) compact(ctx context.Context, sessionID string) {
	turns, err := s.opts.ListTurns(ctx, sessionID, s.opts.MaxHistoryTurns*4)
	if err != nil || len(turns) == 0 {
		return
	}
	utilization := (len(turns) * 100) / max(s.opts.MaxHistoryTurns, 1)
	action := ""
	keep := len(turns)
	switch {
	case utilization >= s.opts.EmergencyThresholdPct:
		action = "emergency_truncate"
		keep = max(s.opts.MaxHistoryTurns/3, s.opts.KeepMinTurns)
	case utilization >= s.opts.AggressiveThresholdPct:
		action = "aggressive_compaction"
		keep = max(s.opts.MaxHistoryTurns/2, s.opts.KeepMinTurns)
	case utilization >= s.opts.BackgroundThresholdPct:
		action = "background_compaction"
		keep = max((s.opts.MaxHistoryTurns*7)/10, s.opts.KeepMinTurns)
	default:
		return
	}
	if keep >= len(turns) {
		return
	}
	removed := append([]Turn(nil), turns[:len(turns)-keep]...)
	removeIDs := make([]string, 0, len(removed))
	for _, turn := range removed {
		if strings.TrimSpace(turn.ID) != "" {
			removeIDs = append(removeIDs, turn.ID)
		}
	}
	if len(removeIDs) == 0 {
		return
	}
	if err := s.opts.DeleteTurns(ctx, sessionID, removeIDs); err != nil {
		return
	}

	summary := ""
	if action != "emergency_truncate" && s.opts.Summarize != nil {
		summary, _ = s.opts.Summarize(ctx, sessionID, removed)
	}
	if strings.TrimSpace(summary) == "" {
		summary = deterministicSummary(removed, 5)
	}
	if s.opts.AppendSummaryMarker != nil {
		_ = s.opts.AppendSummaryMarker(ctx, sessionID, summary)
	}

	run := CompactionRun{
		SessionID:    sessionID,
		ThresholdHit: utilization,
		Action:       action,
		RemovedTurns: len(removeIDs),
		Summary:      summary,
		CreatedAt:    time.Now().UTC(),
	}
	_ = s.persistRun(ctx, run)
}

func (s *Service) persistRun(ctx context.Context, run CompactionRun) error {
	if s == nil || s.store == nil {
		return nil
	}
	data, err := json.Marshal(run)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%d:%s", run.CreatedAt.UnixNano(), strings.TrimSpace(run.SessionID))
	return s.store.PutKV(ctx, kvNamespaceRuns, key, data)
}

func deterministicSummary(turns []Turn, limit int) string {
	if len(turns) == 0 {
		return "Compaction removed old conversation turns."
	}
	if limit <= 0 {
		limit = 5
	}
	if len(turns) > limit {
		turns = turns[len(turns)-limit:]
	}
	lines := []string{"Compaction condensed earlier turns:"}
	for _, turn := range turns {
		content := strings.TrimSpace(turn.Content)
		if len(content) > 140 {
			content = content[:137] + "..."
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", strings.TrimSpace(turn.Role), content))
	}
	return strings.Join(lines, "\n")
}

func sortRuns(runs []CompactionRun) {
	for i := 0; i < len(runs); i++ {
		for j := i + 1; j < len(runs); j++ {
			if runs[j].CreatedAt.After(runs[i].CreatedAt) {
				runs[i], runs[j] = runs[j], runs[i]
			}
		}
	}
}

func max(a, b int) int {
	if a < b {
		return b
	}
	return a
}
