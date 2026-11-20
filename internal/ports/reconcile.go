package ports

import (
	"context"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/liveness"
)

// EffectEnsurer inserts outbox rows for a change without a new event (HARNESS_DESIGN B3). Consumer: the reconciler.
// Adapter: internal/adapters/postgres.
type EffectEnsurer interface {
	// EnsureEffects attaches each effect to the change's newest event. A key that is already committed with the same
	// kind, target and payload is a no-op; one with other content is an error, because a key must mean one action.
	// A change with no events is an error.
	EnsureEffects(ctx context.Context, changeID id.ChangeID, fx []change.Effect) error
}

// OpenChanges lists the changes the reconciler's sweep must visit.
type OpenChanges interface {
	// OpenChanges returns the changes that are not over: no pr.closed without a later pr.reopened, no change.rejected and no reject decision in
	// the log (the same rule as change.View.Over), in id order.
	OpenChanges(ctx context.Context) ([]id.ChangeID, error)
}

// DeadEffects reads the outbox rows of one change that will not run again by themselves.
type DeadEffects interface {
	// DeadEffects returns the change's outbox rows that are not done and either failed eight times or were started
	// and lost their lease (in doubt), in creation order.
	DeadEffects(ctx context.Context, changeID id.ChangeID) ([]change.DeadJob, error)
}

// ModelHealth asks the model provider whether it answers (HARNESS_DESIGN B3, liveness).
type ModelHealth interface {
	// Probe sends one minimal request with the server's own key. A provider that refuses (quota, auth, outage) is
	// liveness.Refusing with no error; an error means the probe itself could not be made.
	Probe(ctx context.Context) (liveness.Health, error)
}
