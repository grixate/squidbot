package bbolt

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.etcd.io/bbolt"

	"github.com/grixate/squidbot/internal/agent"
	"github.com/grixate/squidbot/internal/federation"
	"github.com/grixate/squidbot/internal/mission"
)

func missionTaskKey(id string) string {
	return "task:" + strings.TrimSpace(id)
}

func missionColumnKey(id string) string {
	return "col:" + strings.TrimSpace(id)
}

func usageDayKey(day string) string {
	return "usage:" + strings.TrimSpace(day)
}

func heartbeatRunKey(id string) string {
	return "hbrun:" + strings.TrimSpace(id)
}

func federationRunKey(id string) string {
	return "run:" + strings.TrimSpace(id)
}

func federationPeerHealthKey(peerID string) string {
	return "peer:" + strings.TrimSpace(peerID)
}

func (s *Store) PutMissionTask(ctx context.Context, task mission.Task) error {
	bytes, err := json.Marshal(task)
	if err != nil {
		return err
	}
	return s.runWrite(ctx, func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketMissionTasks).Put([]byte(missionTaskKey(task.ID)), bytes)
	})
}

func (s *Store) DeleteMissionTask(ctx context.Context, id string) error {
	return s.runWrite(ctx, func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketMissionTasks).Delete([]byte(missionTaskKey(id)))
	})
}

func (s *Store) ListMissionTasks(_ context.Context) ([]mission.Task, error) {
	out := make([]mission.Task, 0, 32)
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketMissionTasks).ForEach(func(_, v []byte) error {
			var task mission.Task
			if err := json.Unmarshal(v, &task); err != nil {
				return nil
			}
			out = append(out, task)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ColumnID == out[j].ColumnID && out[i].Position == out[j].Position {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		if out[i].ColumnID == out[j].ColumnID {
			return out[i].Position < out[j].Position
		}
		return out[i].ColumnID < out[j].ColumnID
	})
	return out, nil
}

func (s *Store) ReplaceMissionColumns(ctx context.Context, columns []mission.Column) error {
	return s.runWrite(ctx, func(tx *bbolt.Tx) error {
		if err := tx.DeleteBucket(bucketMissionColumns); err != nil && err != bbolt.ErrBucketNotFound {
			return err
		}
		bucket, err := tx.CreateBucketIfNotExists(bucketMissionColumns)
		if err != nil {
			return err
		}
		for _, column := range columns {
			bytes, marshalErr := json.Marshal(column)
			if marshalErr != nil {
				return marshalErr
			}
			if putErr := bucket.Put([]byte(missionColumnKey(column.ID)), bytes); putErr != nil {
				return putErr
			}
		}
		return nil
	})
}

func (s *Store) ListMissionColumns(_ context.Context) ([]mission.Column, error) {
	out := make([]mission.Column, 0, 8)
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketMissionColumns).ForEach(func(_, v []byte) error {
			var col mission.Column
			if err := json.Unmarshal(v, &col); err != nil {
				return nil
			}
			out = append(out, col)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out, nil
}

func (s *Store) RecordUsageDay(ctx context.Context, day string, promptTokens, completionTokens, totalTokens uint64) error {
	day = strings.TrimSpace(day)
	if day == "" {
		day = time.Now().UTC().Format("2006-01-02")
	}
	return s.runWrite(ctx, func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketUsageDaily)
		key := []byte(usageDayKey(day))
		current := mission.UsageDay{Day: day}
		if existing := bucket.Get(key); existing != nil {
			_ = json.Unmarshal(existing, &current)
		}
		current.PromptTokens += promptTokens
		current.CompletionTokens += completionTokens
		current.TotalTokens += totalTokens
		current.UpdatedAt = time.Now().UTC()
		bytes, err := json.Marshal(current)
		if err != nil {
			return err
		}
		return bucket.Put(key, bytes)
	})
}

func (s *Store) ListUsageDays(_ context.Context) ([]mission.UsageDay, error) {
	out := make([]mission.UsageDay, 0, 32)
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketUsageDaily).ForEach(func(_, v []byte) error {
			var day mission.UsageDay
			if err := json.Unmarshal(v, &day); err != nil {
				return nil
			}
			out = append(out, day)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Day < out[j].Day })
	return out, nil
}

func (s *Store) RecordHeartbeatRun(ctx context.Context, run mission.HeartbeatRun) error {
	bytes, err := json.Marshal(run)
	if err != nil {
		return err
	}
	return s.runWrite(ctx, func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketHeartbeatRuns).Put([]byte(heartbeatRunKey(run.ID)), bytes)
	})
}

func (s *Store) ListHeartbeatRuns(_ context.Context, limit int) ([]mission.HeartbeatRun, error) {
	if limit <= 0 {
		limit = 25
	}
	out := make([]mission.HeartbeatRun, 0, limit)
	err := s.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketHeartbeatRuns)
		cursor := bucket.Cursor()
		for k, v := cursor.Last(); k != nil && len(out) < limit; k, v = cursor.Prev() {
			var run mission.HeartbeatRun
			if err := json.Unmarshal(v, &run); err != nil {
				continue
			}
			out = append(out, run)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out, nil
}

func (s *Store) ListToolEvents(_ context.Context, limit int) ([]agent.ToolEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	out := make([]agent.ToolEvent, 0, limit)
	err := s.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketToolEvents)
		cursor := bucket.Cursor()
		for k, v := cursor.Last(); k != nil && len(out) < limit; k, v = cursor.Prev() {
			var event agent.ToolEvent
			if err := json.Unmarshal(v, &event); err != nil {
				continue
			}
			out = append(out, event)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) ListJobRuns(_ context.Context, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 100
	}
	out := make([]map[string]any, 0, limit)
	err := s.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketJobRuns)
		cursor := bucket.Cursor()
		for k, v := cursor.Last(); k != nil && len(out) < limit; k, v = cursor.Prev() {
			record := map[string]any{}
			if err := json.Unmarshal(v, &record); err != nil {
				continue
			}
			out = append(out, record)
		}
		return nil
	})
	return out, err
}

func (s *Store) PutFederationRun(ctx context.Context, run federation.DelegationRun) error {
	bytes, err := json.Marshal(run)
	if err != nil {
		return err
	}
	return s.runWrite(ctx, func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketFederationRuns).Put([]byte(federationRunKey(run.ID)), bytes)
	})
}

func (s *Store) GetFederationRun(_ context.Context, id string) (federation.DelegationRun, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return federation.DelegationRun{}, fmt.Errorf("run id is required")
	}
	var out federation.DelegationRun
	err := s.db.View(func(tx *bbolt.Tx) error {
		value := tx.Bucket(bucketFederationRuns).Get([]byte(federationRunKey(id)))
		if value == nil {
			return fmt.Errorf("federation run not found")
		}
		return json.Unmarshal(value, &out)
	})
	return out, err
}

func (s *Store) ListFederationRuns(_ context.Context, sessionID string, status federation.DelegationStatus, limit int) ([]federation.DelegationRun, error) {
	sessionID = strings.TrimSpace(sessionID)
	status = federation.DelegationStatus(strings.TrimSpace(strings.ToLower(string(status))))
	out := make([]federation.DelegationRun, 0, 32)
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketFederationRuns).ForEach(func(_, v []byte) error {
			var run federation.DelegationRun
			if err := json.Unmarshal(v, &run); err != nil {
				return nil
			}
			if sessionID != "" && strings.TrimSpace(run.SessionID) != sessionID {
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
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) PutFederationPeerHealth(ctx context.Context, health federation.PeerHealth) error {
	bytes, err := json.Marshal(health)
	if err != nil {
		return err
	}
	return s.runWrite(ctx, func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketFederationPeerState).Put([]byte(federationPeerHealthKey(health.PeerID)), bytes)
	})
}

func (s *Store) GetFederationPeerHealth(_ context.Context, peerID string) (federation.PeerHealth, error) {
	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return federation.PeerHealth{}, fmt.Errorf("peer id is required")
	}
	var out federation.PeerHealth
	err := s.db.View(func(tx *bbolt.Tx) error {
		value := tx.Bucket(bucketFederationPeerState).Get([]byte(federationPeerHealthKey(peerID)))
		if value == nil {
			return fmt.Errorf("federation peer health not found")
		}
		return json.Unmarshal(value, &out)
	})
	return out, err
}

func (s *Store) ListFederationPeerHealth(_ context.Context, limit int) ([]federation.PeerHealth, error) {
	out := make([]federation.PeerHealth, 0, 16)
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketFederationPeerState).ForEach(func(_, v []byte) error {
			var health federation.PeerHealth
			if err := json.Unmarshal(v, &health); err != nil {
				return nil
			}
			out = append(out, health)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
