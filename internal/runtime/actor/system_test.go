package actor

import (
	"context"
	"sync"
	"testing"
	"time"
)

type testHandler struct {
	mu    sync.Mutex
	count int
}

func (h *testHandler) Handle(ctx context.Context, payload interface{}) (interface{}, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.count++
	return h.count, nil
}

func (h *testHandler) Close() error { return nil }

func TestSystemIsolation(t *testing.T) {
	sys := NewSystem(func(sessionID string) (SessionHandler, error) {
		return &testHandler{}, nil
	}, 16, 5*time.Minute)
	defer sys.Stop()

	const sessions = 120
	var wg sync.WaitGroup
	for i := 0; i < sessions; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			session := "s:" + string(rune(id+65))
			for j := 0; j < 5; j++ {
				if _, err := sys.Submit(context.Background(), session, j, true); err != nil {
					t.Errorf("submit failed: %v", err)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	if got := sys.ActorCount(); got == 0 {
		t.Fatalf("expected actors > 0")
	}
}
