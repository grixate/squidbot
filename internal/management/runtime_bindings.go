package management

import (
	"context"
	"time"

	"github.com/grixate/squidbot/internal/heartbeat"
	"github.com/grixate/squidbot/internal/telemetry"
)

type HeartbeatRuntime interface {
	TriggerNow(ctx context.Context) (string, error)
	Interval() time.Duration
	SetInterval(interval time.Duration)
	NextRunAt() (time.Time, bool)
	LastRun() (heartbeat.RunRecord, bool)
	Running() bool
}

type RuntimeBindings struct {
	Metrics   *telemetry.Metrics
	Heartbeat HeartbeatRuntime
}
