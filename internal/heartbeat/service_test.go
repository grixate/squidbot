package heartbeat

import (
	"context"
	"testing"
	"time"
)

func TestIsEmpty(t *testing.T) {
	if !isEmpty("# Header\n\n<!-- comment -->\n") {
		t.Fatal("expected empty content to be treated as empty")
	}
	if isEmpty("# Header\n- [ ]") == false {
		t.Fatal("checkbox-only content should be treated as empty")
	}
	if isEmpty("# Header\n- check inbox") {
		t.Fatal("actionable line should not be empty")
	}
}

func TestSetIntervalUpdatesValue(t *testing.T) {
	service := NewService(t.TempDir(), time.Minute, func(ctx context.Context, prompt string) (string, error) {
		return "ok", nil
	}, nil)
	if got := service.Interval(); got != time.Minute {
		t.Fatalf("expected initial interval 1m, got %s", got)
	}
	service.SetInterval(45 * time.Second)
	if got := service.Interval(); got != 45*time.Second {
		t.Fatalf("expected updated interval 45s, got %s", got)
	}
}
