package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// eventColumns is the select list every event read uses; scanEvent reads them in this order.
const eventColumns = `id, change_id, version, type, actor_kind, actor_id, payload, blob_refs,
	causation_id, correlation_id, occurred_at, prev_hash, hash`

// Load returns every stored event of changeID in version order. A change with no events yields nothing. The
// events come back as stored: the payload is JSONB, so its keys may be in another order, which the canonical form
// ignores, and the hash chain verifies (change.VerifyChain).
func (s *Store) Load(ctx context.Context, changeID id.ChangeID) ([]change.Event, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT "+eventColumns+" FROM event WHERE change_id = $1 ORDER BY version", string(changeID))
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", changeID, err)
	}
	defer rows.Close()
	var out []change.Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", changeID, err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load %s: %w", changeID, err)
	}
	return out, nil
}

// scanEvent reads one row selected with eventColumns into an event.
func scanEvent(row pgx.Row) (change.Event, error) {
	var (
		eid, cid, typ, actorKind, actorID, corr string
		version                                 int64
		payload, prev, hash                     []byte
		blobs                                   []string
		causation                               *string
		occurred                                time.Time
	)
	if err := row.Scan(&eid, &cid, &version, &typ, &actorKind, &actorID, &payload, &blobs, &causation, &corr,
		&occurred, &prev, &hash); err != nil {
		return change.Event{}, fmt.Errorf("read event: %w", err)
	}
	e := change.Event{
		ID:            change.EventID(eid),
		ChangeID:      id.ChangeID(cid),
		Version:       version,
		Type:          change.EventType(typ),
		Actor:         change.Actor{Kind: change.ActorKind(actorKind), ID: change.ActorID(actorID)},
		OccurredAt:    occurred.UTC(),
		CorrelationID: change.CorrelationID(corr),
		Payload:       json.RawMessage(payload),
		BlobRefs:      make([]workflow.BlobRef, 0, len(blobs)),
	}
	for _, b := range blobs {
		e.BlobRefs = append(e.BlobRefs, workflow.BlobRef(b))
	}
	if causation != nil {
		c := change.EventID(*causation)
		e.CausationID = &c
	}
	if prev != nil {
		p, err := hashFromBytes(prev)
		if err != nil {
			return change.Event{}, fmt.Errorf("event %s previous hash: %w", eid, err)
		}
		e.PrevHash = &p
	}
	h, err := hashFromBytes(hash)
	if err != nil {
		return change.Event{}, fmt.Errorf("event %s hash: %w", eid, err)
	}
	e.Hash = h
	return e, nil
}

// hashFromBytes reads a SHA-256 digest stored as BYTEA. A value of any other length is refused, never truncated.
func hashFromBytes(b []byte) (change.Hash, error) {
	var h change.Hash
	if len(b) != len(h) {
		return h, fmt.Errorf("stored hash has %d bytes, want %d", len(b), len(h))
	}
	copy(h[:], b)
	return h, nil
}
