package reconcile

import (
	"context"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Kicker is told which change just changed.
type Kicker interface{ Kick(id.ChangeID) }

// Kicking is an event store that kicks the reconciler after each successful append. A failed append kicks nothing,
// because nothing changed.
type Kicking struct {
	ports.EventStore
	Kicker Kicker
}

// Append appends through the wrapped store and kicks the change once the append has succeeded.
func (k Kicking) Append(ctx context.Context, changeID id.ChangeID, expected int64, evs []change.Event, fx []change.Effect) (int64, error) {
	v, err := k.EventStore.Append(ctx, changeID, expected, evs, fx)
	if err != nil {
		return v, err
	}
	k.Kicker.Kick(changeID)
	return v, nil
}
