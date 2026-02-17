package agent

import (
	"context"

	"github.com/grixate/squidbot/internal/branch"
	"github.com/grixate/squidbot/internal/compaction"
	"github.com/grixate/squidbot/internal/cortex"
)

func (e *Engine) ListBranches(ctx context.Context, sessionID string, limit int) ([]branch.BranchRun, error) {
	if e == nil || e.branches == nil {
		return nil, nil
	}
	return e.branches.List(ctx, sessionID, limit)
}

func (e *Engine) ListCompactionRuns(ctx context.Context, limit int) ([]compaction.CompactionRun, error) {
	if e == nil || e.compactor == nil {
		return nil, nil
	}
	return e.compactor.ListRuns(ctx, limit)
}

func (e *Engine) ListCortexEvents(ctx context.Context, limit int) ([]cortex.Event, error) {
	return e.listCortexEvents(ctx, limit)
}

func (e *Engine) Bulletin(ctx context.Context) string {
	return e.currentCortexBulletin(ctx)
}

func (e *Engine) RegenerateBulletin(ctx context.Context) (string, error) {
	if e == nil || e.cortex == nil {
		return "", nil
	}
	return e.cortex.Regenerate(ctx, "manual")
}
