package subagent

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkManagerParallelFanout(b *testing.B) {
	for n := 0; n < b.N; n++ {
		store := newMemoryStore()
		var idCounter atomic.Int64
		m := NewManager(Options{
			Enabled:        true,
			MaxConcurrent:  4,
			MaxQueue:       32,
			DefaultTimeout: time.Second,
			MaxAttempts:    1,
			NextID: func() string {
				return fmt.Sprintf("bench-%d", idCounter.Add(1))
			},
		}, store, func(ctx context.Context, run Run) (Result, error) {
			time.Sleep(2 * time.Millisecond)
			return Result{Summary: "ok"}, nil
		}, nil, nil)
		if err := m.Start(context.Background()); err != nil {
			b.Fatal(err)
		}
		runIDs := make([]string, 0, 12)
		for i := 0; i < 12; i++ {
			run, err := m.Enqueue(context.Background(), Request{Task: fmt.Sprintf("task-%d", i)})
			if err != nil {
				b.Fatal(err)
			}
			runIDs = append(runIDs, run.ID)
		}
		if _, err := m.Wait(context.Background(), runIDs, 5*time.Second); err != nil {
			b.Fatal(err)
		}
		m.Stop()
	}
}
