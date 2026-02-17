package cortex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	NamespaceBulletin = "runtime_bulletin"
	KeyBulletin       = "current"
	NamespaceEvents   = "runtime_cortex_events"
)

type Event struct {
	ID         string    `json:"id"`
	Trigger    string    `json:"trigger"`
	Status     string    `json:"status"`
	Preview    string    `json:"preview,omitempty"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	FinishedAt time.Time `json:"finished_at"`
}

type KVStore interface {
	PutKV(ctx context.Context, namespace, key string, value []byte) error
	GetKV(ctx context.Context, namespace, key string) ([]byte, error)
	ListKV(ctx context.Context, namespace, prefix string, limit int) (map[string][]byte, error)
}

type Options struct {
	Enabled          bool
	Interval         time.Duration
	BulletinMaxWords int
	Now              func() time.Time
	Synthesize       func(ctx context.Context, maxWords int) (string, string, error)
}

type Service struct {
	store    KVStore
	opts     Options
	stop     chan struct{}
	wg       sync.WaitGroup
	startMu  sync.Mutex
	started  bool
	runMu    sync.Mutex
	lastDone time.Time
}

func NewService(store KVStore, opts Options) *Service {
	if opts.Interval <= 0 {
		opts.Interval = 2 * time.Minute
	}
	if opts.BulletinMaxWords <= 0 {
		opts.BulletinMaxWords = 180
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		store: store,
		opts:  opts,
		stop:  make(chan struct{}),
	}
}

func (s *Service) Start() {
	if s == nil || !s.opts.Enabled || s.opts.Synthesize == nil {
		return
	}
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if s.started {
		return
	}
	s.started = true
	s.wg.Add(1)
	go s.loop()
}

func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.startMu.Lock()
	if !s.started {
		s.startMu.Unlock()
		return
	}
	close(s.stop)
	s.started = false
	s.startMu.Unlock()
	s.wg.Wait()
}

func (s *Service) loop() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.opts.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			_, _ = s.Regenerate(context.Background(), "interval")
		}
	}
}

func (s *Service) Bulletin(ctx context.Context) (string, error) {
	if s == nil || s.store == nil {
		return "", nil
	}
	raw, err := s.store.GetKV(ctx, NamespaceBulletin, KeyBulletin)
	if err != nil || len(raw) == 0 {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

func (s *Service) Regenerate(ctx context.Context, trigger string) (string, error) {
	if s == nil || !s.opts.Enabled || s.opts.Synthesize == nil {
		return "", nil
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()

	trigger = strings.TrimSpace(trigger)
	if trigger == "" {
		trigger = "manual"
	}
	started := s.opts.Now().UTC()
	prev, _ := s.Bulletin(ctx)
	bulletin, model, err := s.opts.Synthesize(ctx, s.opts.BulletinMaxWords)
	finished := s.opts.Now().UTC()

	event := Event{
		ID:         fmt.Sprintf("cortex-%d", started.UnixNano()),
		Trigger:    trigger,
		CreatedAt:  started,
		FinishedAt: finished,
	}
	if err != nil {
		event.Status = "failed"
		event.Error = err.Error()
		event.Preview = truncateWords(prev, 24)
		_ = s.persistEvent(ctx, event)
		s.lastDone = finished
		return prev, err
	}
	bulletin = strings.TrimSpace(truncateWords(bulletin, s.opts.BulletinMaxWords))
	if bulletin == "" {
		bulletin = prev
	}
	if strings.TrimSpace(bulletin) != "" {
		_ = s.store.PutKV(ctx, NamespaceBulletin, KeyBulletin, []byte(bulletin))
	}
	event.Status = "succeeded"
	event.Preview = truncateWords(bulletin, 24)
	if strings.TrimSpace(model) != "" {
		event.Preview = strings.TrimSpace(event.Preview) + " [model=" + strings.TrimSpace(model) + "]"
	}
	_ = s.persistEvent(ctx, event)
	s.lastDone = finished
	return bulletin, nil
}

func (s *Service) Events(ctx context.Context, limit int) ([]Event, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	raw, err := s.store.ListKV(ctx, NamespaceEvents, "", limit*2)
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(raw))
	for _, item := range raw {
		var event Event
		if err := json.Unmarshal(item, &event); err != nil {
			continue
		}
		out = append(out, event)
	}
	sortEvents(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Service) persistEvent(ctx context.Context, event Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return s.store.PutKV(ctx, NamespaceEvents, fmt.Sprintf("%d:%s", event.CreatedAt.UnixNano(), event.ID), data)
}

func truncateWords(text string, maxWords int) string {
	text = strings.TrimSpace(text)
	if text == "" || maxWords <= 0 {
		return text
	}
	words := strings.Fields(text)
	if len(words) <= maxWords {
		return strings.Join(words, " ")
	}
	return strings.Join(words[:maxWords], " ") + "..."
}

func sortEvents(events []Event) {
	for i := 0; i < len(events); i++ {
		for j := i + 1; j < len(events); j++ {
			if events[j].CreatedAt.After(events[i].CreatedAt) {
				events[i], events[j] = events[j], events[i]
			}
		}
	}
}
