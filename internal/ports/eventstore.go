package ports

import (
	"context"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// EventStore is the append-only log of one change (TECH_SPEC 2.3, 4.1). Consumer: the use cases in internal/app.
// Adapter: internal/adapters/postgres (task T20).
type EventStore interface {
	// Append writes evs and fx in one transaction after checking that the change is still at expected. It returns
	// the new version, or ErrVersionConflict when another writer got there first.
	Append(ctx context.Context, changeID id.ChangeID, expected int64, evs []change.Event, fx []change.Effect) (int64, error)
	// Load returns every event of the change in version order.
	Load(ctx context.Context, changeID id.ChangeID) ([]change.Event, error)
}

// Outbox drains the effects that Append committed (TECH_SPEC 2.4). Consumer: the outbox drainer in internal/app.
// Adapter: internal/adapters/postgres (task T20, drainer T22).
type Outbox interface {
	// Pending returns up to limit effects that are not done yet, oldest first.
	Pending(ctx context.Context, limit int) ([]change.Effect, error)
	// Done marks an effect as performed. Calling it twice is not an error.
	Done(ctx context.Context, key change.EffectKey) error
	// Fail records one failed attempt and its reason. After eight attempts the effect goes to the dead-letter view.
	Fail(ctx context.Context, key change.EffectKey, reason string) error
}
