package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Append commits evs and fx in one transaction (TECH_SPEC 2.4). Under the change's advisory lock it checks that the
// change is at expected, computes the hash chain of evs from the stored head, inserts the events and then the
// effects. The effects are attributed to the last event of the batch, which is the one that decided them.
func (s *Store) Append(ctx context.Context, changeID id.ChangeID, expected int64, evs []change.Event, fx []change.Effect) (int64, error) {
	prepared, err := prepare(changeID, expected, evs, fx)
	if err != nil {
		return 0, fmt.Errorf("append to %s: %w", changeID, err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("append to %s: begin: %w", changeID, err)
	}
	// A rollback after a successful commit reports pgx.ErrTxClosed, which is the expected outcome here. The rollback
	// runs on a context that survives cancellation, so a cancelled append still releases its transaction.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", string(changeID)); err != nil {
		return 0, fmt.Errorf("append to %s: lock: %w", changeID, err)
	}
	head, prev, err := storedHead(ctx, tx, changeID)
	if err != nil {
		return 0, fmt.Errorf("append to %s: %w", changeID, err)
	}
	if head != expected {
		return 0, fmt.Errorf("append to %s: change is at version %d, not %d: %w", changeID, head, expected, ports.ErrVersionConflict)
	}

	stored, err := chainEvents(prepared, prev, expected == 0)
	if err != nil {
		return 0, fmt.Errorf("append to %s: %w", changeID, err)
	}
	for _, e := range stored {
		if err := insertEvent(ctx, tx, e); err != nil {
			return 0, fmt.Errorf("append to %s: insert version %d: %w", changeID, e.Version, translateAppendErr(err))
		}
	}
	causing := stored[len(stored)-1].ID
	for _, f := range fx {
		if err := insertEffect(ctx, tx, causing, f); err != nil {
			return 0, fmt.Errorf("append to %s: effect %s: %w", changeID, f.Key, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("append to %s: commit: %w", changeID, err)
	}
	return expected + int64(len(stored)), nil
}

// prepare checks the batch without touching the database and returns the events in the form they are stored: the
// occurred_at cut to microseconds (what Postgres keeps, so the hash covers what is read back), and no previous hash
// or hash yet. The caller's values for those two fields are replaced.
func prepare(changeID id.ChangeID, expected int64, evs []change.Event, fx []change.Effect) ([]change.Event, error) {
	if _, err := id.ParseChangeID(string(changeID)); err != nil {
		return nil, err
	}
	if expected < 0 {
		return nil, fmt.Errorf("expected version %d is negative", expected)
	}
	if len(evs) == 0 {
		return nil, errors.New("no events: an append needs at least one event, and an effect is caused by one")
	}
	out := make([]change.Event, len(evs))
	for i, e := range evs {
		want := expected + int64(i) + 1
		if e.ChangeID != changeID {
			return nil, fmt.Errorf("event at version %d is for change %s", want, e.ChangeID)
		}
		if e.Version != want {
			return nil, fmt.Errorf("event %d has version %d, want %d", i+1, e.Version, want)
		}
		e.OccurredAt = e.OccurredAt.UTC().Truncate(time.Microsecond)
		if err := e.Validate(); err != nil {
			return nil, fmt.Errorf("event at version %d: %w", want, err)
		}
		e.PrevHash = nil
		e.Hash = change.Hash{}
		out[i] = e
	}
	for _, f := range fx {
		if err := f.Validate(); err != nil {
			return nil, fmt.Errorf("effect %s: %w", f.Key, err)
		}
	}
	return out, nil
}

// storedHead returns the version and hash of the newest stored event of changeID, or 0 and the zero hash when the
// change has no events.
func storedHead(ctx context.Context, tx pgx.Tx, changeID id.ChangeID) (int64, change.Hash, error) {
	var version int64
	var hash []byte
	err := tx.QueryRow(ctx,
		"SELECT version, hash FROM event WHERE change_id = $1 ORDER BY version DESC LIMIT 1",
		string(changeID)).Scan(&version, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, change.Hash{}, nil
	}
	if err != nil {
		return 0, change.Hash{}, fmt.Errorf("read head: %w", err)
	}
	h, err := hashFromBytes(hash)
	if err != nil {
		return 0, change.Hash{}, fmt.Errorf("read head: %w", err)
	}
	return version, h, nil
}

// chainEvents sets PrevHash and Hash on each event, starting after prev. A genesis batch (first is true) has no
// previous hash, as VerifyChain requires.
func chainEvents(evs []change.Event, prev change.Hash, first bool) ([]change.Event, error) {
	out := make([]change.Event, len(evs))
	for i, e := range evs {
		if i == 0 && first {
			e.PrevHash = nil
		} else {
			p := prev
			e.PrevHash = &p
		}
		h, err := change.Chain(prev, e)
		if err != nil {
			return nil, fmt.Errorf("hash version %d: %w", e.Version, err)
		}
		e.Hash = h
		prev = h
		out[i] = e
	}
	return out, nil
}

// insertEvent writes one event row.
func insertEvent(ctx context.Context, tx pgx.Tx, e change.Event) error {
	blobs := make([]string, 0, len(e.BlobRefs))
	for _, b := range e.BlobRefs {
		blobs = append(blobs, string(b))
	}
	var causation *string
	if e.CausationID != nil {
		c := string(*e.CausationID)
		causation = &c
	}
	var prev []byte
	if e.PrevHash != nil {
		prev = e.PrevHash[:]
	}
	hash := e.Hash
	_, err := tx.Exec(ctx, `INSERT INTO event
		(id, change_id, version, type, actor_kind, actor_id, payload, blob_refs, causation_id, correlation_id,
		 occurred_at, prev_hash, hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		string(e.ID), string(e.ChangeID), e.Version, string(e.Type), string(e.Actor.Kind), string(e.Actor.ID),
		string(e.Payload), blobs, causation, string(e.CorrelationID), e.OccurredAt, prev, hash[:])
	return err
}

// insertEffect writes one outbox row. A key that is already committed is a no-op when the content is the same, and
// a refusal when it differs: the same key must always mean the same action (TECH_SPEC 2.4).
func insertEffect(ctx context.Context, tx pgx.Tx, causing change.EventID, f change.Effect) error {
	tag, err := tx.Exec(ctx, `INSERT INTO outbox (effect_key, kind, target, payload, event_id)
		VALUES ($1, $2, $3, $4::jsonb, $5)
		ON CONFLICT (effect_key) DO NOTHING`,
		string(f.Key), string(f.Kind), f.Target, string(f.Payload), string(causing))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT kind = $2 AND target = $3 AND payload = $4::jsonb
		FROM outbox WHERE effect_key = $1`,
		string(f.Key), string(f.Kind), f.Target, string(f.Payload)).Scan(&same)
	if err != nil {
		return fmt.Errorf("read committed effect: %w", err)
	}
	if !same {
		return fmt.Errorf("key %s already committed with different content: %w", f.Key, errEffectKeyReused)
	}
	return nil
}
