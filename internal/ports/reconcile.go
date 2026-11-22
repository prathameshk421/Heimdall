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

// OpsSnapshot summarizes server operational health without SQL (O6): outbox queue depth and oldest age per kind,
// dead and in-doubt job counts, dead chat messages, and open change count.
type OpsSnapshot struct {
	QueuedByKind        map[change.EffectKind]int   `json:"queued_by_kind"`
	OldestQueuedSeconds map[change.EffectKind]int64 `json:"oldest_queued_seconds"`
	DeadByKind          map[change.EffectKind]int   `json:"dead_by_kind"`
	DeadCount           int                         `json:"dead_count"`
	InDoubtCount        int                         `json:"in_doubt_count"`
	DeadMessages        int                         `json:"dead_messages"`
	OpenChanges         int                         `json:"open_changes"`
}

// OpsReader reads an OpsSnapshot from the durable store (O6).
type OpsReader interface {
	OpsSnapshot(ctx context.Context) (OpsSnapshot, error)
}
