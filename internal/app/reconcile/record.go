package reconcile

import (
	"context"
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// maxRecordTries is how many times Record builds again after a version conflict.
const maxRecordTries = 5

// Recorder is the one way a component other than the reconciler appends a fact: intake, prsync, the trigger, agent
// tools, operator commands and executors all record through it. Hand it the event store wrapped in Kicking, so the
// reconciler wakes after each append.
type Recorder struct {
	Store ports.EventStore
}

// Record loads the change, folds it, calls build with the view and appends the events build returns. build sets each
// event's id, type, actor, time and payload; Record numbers them, sets the change id and fills CausationID and
// CorrelationID (from the change's last event) when they are empty. A version conflict loads again and builds again.
// build returning no events appends nothing. A fact recorded twice folds to the same view, so an executor that crashed
// after the append may record again.
func (r Recorder) Record(ctx context.Context, changeID id.ChangeID, build func(change.View) ([]change.Event, error)) error {
	for try := 1; try <= maxRecordTries; try++ {
		err := r.recordOnce(ctx, changeID, build)
		if !errors.Is(err, ports.ErrVersionConflict) {
			return err
		}
	}
	return fmt.Errorf("record on %s: still conflicting after %d tries: %w", changeID, maxRecordTries, ports.ErrVersionConflict)
}

func (r Recorder) recordOnce(ctx context.Context, changeID id.ChangeID, build func(change.View) ([]change.Event, error)) error {
	events, err := r.Store.Load(ctx, changeID)
	if err != nil {
		return fmt.Errorf("record on %s: load: %w", changeID, err)
	}
	v, err := change.Fold(events)
	if err != nil {
		return fmt.Errorf("record on %s: fold: %w", changeID, err)
	}
	built, err := build(v)
	if err != nil {
		return fmt.Errorf("record on %s: %w", changeID, err)
	}
	if len(built) == 0 {
		return nil
	}
	version := int64(len(events))
	numbered := number(changeID, events, built)
	if _, err := r.Store.Append(ctx, changeID, version, numbered, nil); err != nil {
		return fmt.Errorf("record on %s: %w", changeID, err)
	}
	return nil
}

// number gives the events their change id and versions after the stored ones and fills the causation and correlation
// that are empty: the last event before each one is its cause, and the chain keeps one correlation.
func number(changeID id.ChangeID, stored, built []change.Event) []change.Event {
	out := make([]change.Event, len(built))
	var last *change.Event
	if len(stored) > 0 {
		last = &stored[len(stored)-1]
	}
	for i, e := range built {
		e.ChangeID = changeID
		e.Version = int64(len(stored)+i) + 1
		if last != nil {
			if e.CausationID == nil {
				cause := last.ID
				e.CausationID = &cause
			}
			if e.CorrelationID == "" {
				e.CorrelationID = last.CorrelationID
			}
		}
		out[i] = e
		last = &out[i]
	}
	return out
}

// Append records events that are already built, whatever the view says. It is Record for a writer that decides
// nothing from the view.
func (r Recorder) Append(ctx context.Context, changeID id.ChangeID, evs []change.Event) error {
	return r.Record(ctx, changeID, func(change.View) ([]change.Event, error) { return evs, nil })
}
