package bbolt

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/subagent"
	"go.etcd.io/bbolt"
)

func subagentRunKey(id string) string {
	return "run:" + strings.TrimSpace(id)
}

func subagentEventKey(event subagent.Event) string {
	when := event.CreatedAt.UTC()
	return fmt.Sprintf("event:%s:%s:%s", strings.TrimSpace(event.RunID), when.Format(time.RFC3339Nano), strings.TrimSpace(event.ID))
}

func (s *Store) PutSubagentRun(ctx context.Context, run subagent.Run) error {
	bytes, err := json.Marshal(run)
	if err != nil {
		return err
	}
	return s.runWrite(ctx, func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketSubagentRuns).Put([]byte(subagentRunKey(run.ID)), bytes)
	})
}

func (s *Store) GetSubagentRun(_ context.Context, id string) (subagent.Run, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return subagent.Run{}, fmt.Errorf("run id is required")
	}
	var out subagent.Run
	err := s.db.View(func(tx *bbolt.Tx) error {
		value := tx.Bucket(bucketSubagentRuns).Get([]byte(subagentRunKey(id)))
		if value == nil {
			return fmt.Errorf("subagent run not found")
		}
		return json.Unmarshal(value, &out)
	})
	return out, err
}

func (s *Store) ListSubagentRunsBySession(_ context.Context, sessionID string, limit int) ([]subagent.Run, error) {
	sessionID = strings.TrimSpace(sessionID)
	out := make([]subagent.Run, 0, 32)
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketSubagentRuns).ForEach(func(_, v []byte) error {
			var run subagent.Run
			if err := json.Unmarshal(v, &run); err != nil {
				return nil
			}
			if sessionID != "" && run.SessionID != sessionID {
				return nil
			}
			out = append(out, run)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) ListSubagentRunsByStatus(_ context.Context, status subagent.Status, limit int) ([]subagent.Run, error) {
	status = subagent.Status(strings.TrimSpace(strings.ToLower(string(status))))
	out := make([]subagent.Run, 0, 32)
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketSubagentRuns).ForEach(func(_, v []byte) error {
			var run subagent.Run
			if err := json.Unmarshal(v, &run); err != nil {
				return nil
			}
			if status != "" && run.Status != status {
				return nil
			}
			out = append(out, run)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) AppendSubagentEvent(ctx context.Context, event subagent.Event) error {
	if strings.TrimSpace(event.ID) == "" {
		event.ID = s.nextULID()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	bytes, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return s.runWrite(ctx, func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketSubagentEvents).Put([]byte(subagentEventKey(event)), bytes)
	})
}
