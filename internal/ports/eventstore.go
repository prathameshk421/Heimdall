package ports

import (
	"context"
	"errors"
	"time"

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
	// Pending returns up to limit effects that are not done yet, oldest first. It only reads; it takes no lease.
	Pending(ctx context.Context, limit int) ([]change.Effect, error)
	// Claim takes a lease of lease length on up to limit undone effects that have not started and whose lease is free
	// or expired, oldest first, and returns them. Two live leases never cover the same effect. A started effect whose
	// lease ran out is in doubt and is never returned again (T22).
	Claim(ctx context.Context, limit int, lease time.Duration) ([]change.Effect, error)
	// ClaimEffect claims exactly the keyed effect. It returns (nil, nil) when that key is done, exhausted or in doubt.
	// ErrEffectLeased means another worker owns a live lease and the caller should retry this job.
	ClaimEffect(ctx context.Context, key change.EffectKey, lease time.Duration) (*change.Effect, error)
	// Start records, before the executor runs, that the effect is about to run. It succeeds only under a live lease on
	// an effect that has not started; any other call is refused and the caller must not run the executor (T22).
	Start(ctx context.Context, key change.EffectKey) error
	// Done marks an effect as performed. Calling it twice is not an error.
	Done(ctx context.Context, key change.EffectKey) error
	// Fail records one failed attempt and its reason and clears the start mark, so the effect may run again. After
	// eight attempts the effect goes to the dead-letter view.
	Fail(ctx context.Context, key change.EffectKey, reason string) error
}

// ErrEffectLeased is returned when a keyed effect is held by another worker's live lease.
var ErrEffectLeased = errors.New("effect has a live lease")

// OpenPRStream is one change whose latest pull request has no pr.closed event in the log.
type OpenPRStream struct {
	Change id.ChangeID
	Repo   change.Repo
	Number int
}

// PRStreams lists the changes whose pull request is still open in the event log, so the reconciler can ask GitHub
// about each one (TECH_SPEC 2.2, D56). Consumer: internal/app/prsync. Adapter: internal/adapters/postgres.
type PRStreams interface {
	OpenPRStreams(ctx context.Context) ([]OpenPRStream, error)
}
