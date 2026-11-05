package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Store is the Postgres adapter of ports.EventStore and ports.Outbox, and of the cursor and webhook tables.
type Store struct {
	pool     *pgxpool.Pool
	enqueuer EffectEnqueuer
}

// EffectEnqueuer inserts an effect job through the event append transaction.
type EffectEnqueuer interface {
	EnqueueEffectTx(context.Context, pgx.Tx, change.Effect) error
}

// SetEffectEnqueuer enables transactional River delivery for newly appended effects.
func (s *Store) SetEffectEnqueuer(enqueuer EffectEnqueuer) { s.enqueuer = enqueuer }

var (
	_ ports.EventStore = (*Store)(nil)
	_ ports.Outbox     = (*Store)(nil)
	_ ports.RoomIndex  = (*Store)(nil)
)

// NewStore wraps a pool whose database has been migrated with Migrate.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// VerifyReport is the result of VerifyAll.
type VerifyReport struct {
	// Verified lists the changes whose chain checks out, with their heads.
	Verified []ChangeHead
	// Broken lists the changes whose chain does not check out, with the reason.
	Broken []BrokenChange
}

// ChangeHead is the verified head of one change: its last version and the hash of that event in lowercase hex.
type ChangeHead struct {
	ChangeID    id.ChangeID
	LastVersion int64
	HeadHash    string
}

// BrokenChange names a change whose chain does not verify, and why.
type BrokenChange struct {
	ChangeID id.ChangeID
	Err      error
}
