package bbolt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	mrand "math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"go.etcd.io/bbolt"

	"github.com/grixate/squidbot/internal/agent"
	"github.com/grixate/squidbot/internal/provider"
)

var (
	bucketSessions         = []byte("sessions")
	bucketTurns            = []byte("turns")
	bucketToolEvents       = []byte("tool_events")
	bucketJobs             = []byte("jobs")
	bucketJobRuns          = []byte("job_runs")
	bucketKV               = []byte("kv")
	bucketActorCheckpoints = []byte("actor_checkpoints")
	bucketSchemaMigrations = []byte("schema_migrations")
	bucketMissionTasks     = []byte("mission_tasks")
	bucketMissionColumns   = []byte("mission_columns")
	bucketMissionPolicy    = []byte("mission_policy")
	bucketUsageDaily       = []byte("usage_daily")
	bucketHeartbeatRuns    = []byte("heartbeat_runs")
)

type writeTask struct {
	ctx  context.Context
	fn   func(tx *bbolt.Tx) error
	done chan error
}

type Store struct {
	db      *bbolt.DB
	writes  chan writeTask
	stop    chan struct{}
	wg      sync.WaitGroup
	entropy *ulid.MonotonicEntropy
	mu      sync.Mutex
}

func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("db path required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := bbolt.Open(path, 0o600, &bbolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, err
	}

	store := &Store{
		db:      db,
		writes:  make(chan writeTask, 128),
		stop:    make(chan struct{}),
		entropy: ulid.Monotonic(mrand.New(mrand.NewSource(time.Now().UnixNano())), 0),
	}

	if err := store.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}

	store.wg.Add(1)
	go store.writer()
	return store, nil
}

func (s *Store) Close() error {
	close(s.stop)
	s.wg.Wait()
	return s.db.Close()
}

func (s *Store) initSchema() error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		buckets := [][]byte{
			bucketSessions,
			bucketTurns,
			bucketToolEvents,
			bucketJobs,
			bucketJobRuns,
			bucketKV,
			bucketActorCheckpoints,
			bucketSchemaMigrations,
			bucketMissionTasks,
			bucketMissionColumns,
			bucketMissionPolicy,
			bucketUsageDaily,
			bucketHeartbeatRuns,
		}
		for _, b := range buckets {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}
		migrations := tx.Bucket(bucketSchemaMigrations)
		return migrations.Put([]byte("schema_version"), []byte("1"))
	})
}

func (s *Store) writer() {
	defer s.wg.Done()
	for {
		select {
		case <-s.stop:
			return
		case task := <-s.writes:
			err := s.db.Update(func(tx *bbolt.Tx) error {
				return task.fn(tx)
			})
			select {
			case task.done <- err:
			default:
			}
		}
	}
}

func (s *Store) runWrite(ctx context.Context, fn func(tx *bbolt.Tx) error) error {
	t := writeTask{ctx: ctx, fn: fn, done: make(chan error, 1)}
	select {
	case s.writes <- t:
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-t.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Store) nextULID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ulid.MustNew(ulid.Timestamp(time.Now()), s.entropy).String()
}

func turnKey(sessionID, id string) string {
	return fmt.Sprintf("turn:%s:%s", sessionID, id)
}

func sessionKey(sessionID string) string {
	return fmt.Sprintf("sess:%s", sessionID)
}

func kvKey(namespace, key string) string {
	return fmt.Sprintf("kv:%s:%s", namespace, key)
}

func jobKey(id string) string {
	return fmt.Sprintf("job:%s", id)
}

func jobRunKey(id string) string {
	return fmt.Sprintf("jobrun:%s", id)
}

func checkpointKey(sessionID string) string {
	return fmt.Sprintf("checkpoint:%s", sessionID)
}

func (s *Store) AppendTurn(ctx context.Context, turn agent.Turn) error {
	if turn.ID == "" {
		turn.ID = s.nextULID()
	}
	if turn.CreatedAt.IsZero() {
		turn.CreatedAt = time.Now().UTC()
	}
	if turn.Version == 0 {
		turn.Version = 1
	}
	bytes, err := json.Marshal(turn)
	if err != nil {
		return err
	}
	return s.runWrite(ctx, func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketTurns)
		return bucket.Put([]byte(turnKey(turn.SessionID, turn.ID)), bytes)
	})
}

func (s *Store) Window(ctx context.Context, sessionID string, limit int) ([]provider.Message, error) {
	if limit <= 0 {
		limit = 50
	}
	prefix := []byte("turn:" + sessionID + ":")
	turns := make([]agent.Turn, 0, limit)
	err := s.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketTurns)
		cursor := bucket.Cursor()
		for key, value := cursor.Seek(prefix); key != nil && strings.HasPrefix(string(key), string(prefix)); key, value = cursor.Next() {
			var turn agent.Turn
			if err := json.Unmarshal(value, &turn); err != nil {
				continue
			}
			turns = append(turns, turn)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(turns, func(i, j int) bool {
		return turns[i].CreatedAt.Before(turns[j].CreatedAt)
	})
	if len(turns) > limit {
		turns = turns[len(turns)-limit:]
	}
	messages := make([]provider.Message, 0, len(turns))
	for _, turn := range turns {
		messages = append(messages, provider.Message{
			Role:       turn.Role,
			Content:    turn.Content,
			Name:       turn.Name,
			ToolCallID: turn.ToolCallID,
			ToolCalls:  turn.ToolCalls,
		})
	}
	return messages, nil
}

func (s *Store) SaveSessionMeta(ctx context.Context, sessionID string, meta map[string]any) error {
	record := map[string]any{"session_id": sessionID, "meta": meta, "updated_at": time.Now().UTC(), "version": 1}
	bytes, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return s.runWrite(ctx, func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketSessions).Put([]byte(sessionKey(sessionID)), bytes)
	})
}

func (s *Store) AppendToolEvent(ctx context.Context, event agent.ToolEvent) error {
	if event.ID == "" {
		event.ID = s.nextULID()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if event.Version == 0 {
		event.Version = 1
	}
	bytes, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return s.runWrite(ctx, func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketToolEvents).Put([]byte("event:"+event.ID), bytes)
	})
}

func (s *Store) PutKV(ctx context.Context, namespace, key string, value []byte) error {
	return s.runWrite(ctx, func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketKV).Put([]byte(kvKey(namespace, key)), value)
	})
}

func (s *Store) GetKV(_ context.Context, namespace, key string) ([]byte, error) {
	var out []byte
	err := s.db.View(func(tx *bbolt.Tx) error {
		value := tx.Bucket(bucketKV).Get([]byte(kvKey(namespace, key)))
		if value == nil {
			return bbolt.ErrBucketNotFound
		}
		out = append([]byte(nil), value...)
		return nil
	})
	if errors.Is(err, bbolt.ErrBucketNotFound) {
		return nil, fmt.Errorf("kv value not found")
	}
	return out, err
}

func (s *Store) PutJob(ctx context.Context, job []byte, id string) error {
	return s.runWrite(ctx, func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketJobs).Put([]byte(jobKey(id)), job)
	})
}

func (s *Store) DeleteJob(ctx context.Context, id string) error {
	return s.runWrite(ctx, func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketJobs).Delete([]byte(jobKey(id)))
	})
}

func (s *Store) ListJobs(_ context.Context) (map[string][]byte, error) {
	out := map[string][]byte{}
	err := s.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketJobs)
		return bucket.ForEach(func(k, v []byte) error {
			out[string(k)] = append([]byte(nil), v...)
			return nil
		})
	})
	return out, err
}

func (s *Store) RecordJobRun(ctx context.Context, runID string, payload []byte) error {
	return s.runWrite(ctx, func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketJobRuns).Put([]byte(jobRunKey(runID)), payload)
	})
}

func (s *Store) SaveCheckpoint(ctx context.Context, sessionID string, payload []byte) error {
	return s.runWrite(ctx, func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketActorCheckpoints).Put([]byte(checkpointKey(sessionID)), payload)
	})
}

func (s *Store) LoadCheckpoint(_ context.Context, sessionID string) ([]byte, error) {
	var out []byte
	err := s.db.View(func(tx *bbolt.Tx) error {
		value := tx.Bucket(bucketActorCheckpoints).Get([]byte(checkpointKey(sessionID)))
		if value == nil {
			return fmt.Errorf("checkpoint not found")
		}
		out = append([]byte(nil), value...)
		return nil
	})
	return out, err
}
