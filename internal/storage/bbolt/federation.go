package bbolt

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/federation"
	"go.etcd.io/bbolt"
)

func federationRunKey(id string) string {
	return "run:" + strings.TrimSpace(id)
}

func federationEventKey(event federation.Event) string {
	when := event.CreatedAt.UTC()
	return fmt.Sprintf("event:%s:%s:%s", strings.TrimSpace(event.RunID), when.Format(time.RFC3339Nano), strings.TrimSpace(event.ID))
}

func federationIdemKey(originNodeID, idempotencyKey string) string {
	return fmt.Sprintf("idem:%s:%s", strings.TrimSpace(originNodeID), strings.TrimSpace(idempotencyKey))
}

func federationPeerHealthKey(peerID string) string {
	return "peer:" + strings.TrimSpace(peerID)
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
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) AppendFederationEvent(ctx context.Context, event federation.Event) error {
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
		return tx.Bucket(bucketFederationEvents).Put([]byte(federationEventKey(event)), bytes)
	})
}

func (s *Store) PutFederationIdempotency(ctx context.Context, record federation.IdempotencyRecord) error {
	record.OriginNodeID = strings.TrimSpace(record.OriginNodeID)
	record.IdempotencyKey = strings.TrimSpace(record.IdempotencyKey)
	if record.OriginNodeID == "" || record.IdempotencyKey == "" {
		return fmt.Errorf("origin_node_id and idempotency_key are required")
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	bytes, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return s.runWrite(ctx, func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketFederationIdem).Put([]byte(federationIdemKey(record.OriginNodeID, record.IdempotencyKey)), bytes)
	})
}

func (s *Store) GetFederationIdempotency(ctx context.Context, originNodeID, idempotencyKey string) (federation.IdempotencyRecord, error) {
	originNodeID = strings.TrimSpace(originNodeID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if originNodeID == "" || idempotencyKey == "" {
		return federation.IdempotencyRecord{}, fmt.Errorf("origin_node_id and idempotency_key are required")
	}
	var out federation.IdempotencyRecord
	err := s.db.View(func(tx *bbolt.Tx) error {
		value := tx.Bucket(bucketFederationIdem).Get([]byte(federationIdemKey(originNodeID, idempotencyKey)))
		if value == nil {
			return fmt.Errorf("federation idempotency record not found")
		}
		return json.Unmarshal(value, &out)
	})
	if err != nil {
		return federation.IdempotencyRecord{}, err
	}
	if !out.ExpiresAt.IsZero() && out.ExpiresAt.Before(time.Now().UTC()) {
		_ = s.DeleteFederationIdempotency(ctx, originNodeID, idempotencyKey)
		return federation.IdempotencyRecord{}, fmt.Errorf("federation idempotency record expired")
	}
	return out, nil
}

func (s *Store) DeleteFederationIdempotency(ctx context.Context, originNodeID, idempotencyKey string) error {
	return s.runWrite(ctx, func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketFederationIdem).Delete([]byte(federationIdemKey(originNodeID, idempotencyKey)))
	})
}

func (s *Store) PutFederationPeerHealth(ctx context.Context, health federation.PeerHealth) error {
	health.PeerID = strings.TrimSpace(health.PeerID)
	if health.PeerID == "" {
		return fmt.Errorf("peer id is required")
	}
	if health.UpdatedAt.IsZero() {
		health.UpdatedAt = time.Now().UTC()
	}
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
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

