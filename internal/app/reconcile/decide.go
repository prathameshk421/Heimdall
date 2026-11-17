package reconcile

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// decision is what one Run does: the facts to append and the jobs to attach.
type decision struct {
	facts []change.Payload
	jobs  []change.Effect
}

func (d *decision) fact(p change.Payload) { d.facts = append(d.facts, p) }

func (d *decision) job(j change.Job) error {
	raw, err := json.Marshal(j.Payload)
	if err != nil {
		return fmt.Errorf("encode %s payload: %w", j.Kind, err)
	}
	d.jobs = append(d.jobs, change.Effect{Key: j.Key, Kind: j.Kind, Target: j.Target, Payload: raw})
	return nil
}

// decide reads the actions of every open row and turns them into facts and jobs. Asks and texts need nothing done:
// they are what Missing says.
func (r *Reconciler) decide(ctx context.Context, in change.In) (decision, error) {
	var d decision
	for _, ra := range change.Next(in) {
		switch {
		case ra.Fact != nil:
			d.fact(ra.Fact)
		case ra.Job != nil:
			if err := d.job(*ra.Job); err != nil {
				return d, fmt.Errorf("row %s: %w", ra.Row, err)
			}
		case ra.Task != nil:
			if err := r.task(ctx, &d, in, *ra.Task); err != nil {
				return d, fmt.Errorf("row %s: %w", ra.Row, err)
			}
		}
	}
	return d, nil
}

// apply appends the facts with the jobs attached to the last one, or only attaches the jobs when there is no fact.
func (r *Reconciler) apply(ctx context.Context, changeID id.ChangeID, events []change.Event, d decision) error {
	switch {
	case len(d.facts) > 0:
		built, err := r.events(d.facts)
		if err != nil {
			return fmt.Errorf("reconcile %s: %w", changeID, err)
		}
		numbered := number(changeID, events, built)
		if _, err := r.cfg.Store.Append(ctx, changeID, int64(len(events)), numbered, d.jobs); err != nil {
			return fmt.Errorf("reconcile %s: append: %w", changeID, err)
		}
	case len(d.jobs) > 0:
		if err := r.cfg.Effects.EnsureEffects(ctx, changeID, d.jobs); err != nil {
			return fmt.Errorf("reconcile %s: ensure jobs: %w", changeID, err)
		}
	}
	return nil
}

// events wraps each payload in an event of the reconciler. Version, change id, causation and correlation are set when
// the batch is numbered.
func (r *Reconciler) events(facts []change.Payload) ([]change.Event, error) {
	now := r.cfg.Clock.Now()
	out := make([]change.Event, 0, len(facts))
	for _, p := range facts {
		typ, ok := change.TypeOf(p)
		if !ok {
			return nil, fmt.Errorf("fact %T is not in the event catalog", p)
		}
		raw, err := json.Marshal(p)
		if err != nil {
			return nil, fmt.Errorf("encode %s: %w", typ, err)
		}
		out = append(out, change.Event{ID: r.cfg.IDs.NewEvent(), Type: typ, Actor: change.Actor{Kind: change.ActorSystem, ID: "heimdall"}, OccurredAt: now,
			CorrelationID: "", Payload: raw})
	}
	return out, nil
}
