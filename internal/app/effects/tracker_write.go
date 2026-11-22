package effects

import (
	"context"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const trackerActorID = change.ActorID("jira")

// TrackerWriteDeps are the dependencies of TrackerWrite.
type TrackerWriteDeps struct {
	Writer       ports.TicketWriter
	Transitioner ports.TicketTransitioner // optional; used when the payload names a transition
	Ingest       Ingest
	IDs          ports.IDs
	Clock        ports.Clock
}

// TrackerWrite performs tracker_write effects: it comments on the ticket, optionally transitions its status, and
// records tracker.updated (F1).
type TrackerWrite struct {
	deps TrackerWriteDeps
}

// NewTrackerWrite builds the executor, or refuses when a required dependency is missing.
func NewTrackerWrite(d TrackerWriteDeps) (TrackerWrite, error) {
	var missing []string
	if d.Writer == nil {
		missing = append(missing, "writer")
	}
	if d.Ingest == nil {
		missing = append(missing, "ingest")
	}
	if d.IDs == nil {
		missing = append(missing, "ids")
	}
	if d.Clock == nil {
		missing = append(missing, "clock")
	}
	if len(missing) > 0 {
		return TrackerWrite{}, fmt.Errorf("tracker_write executor: missing %v", missing)
	}
	return TrackerWrite{deps: d}, nil
}

// Execute writes the comment and optional transition to the tracker, then records tracker.updated.
func (t TrackerWrite) Execute(ctx context.Context, fx change.Effect) error {
	if fx.Kind != change.EffectTrackerWrite {
		return failf(nil, "tracker_write executor: effect %s has kind %s", fx.Key, fx.Kind)
	}
	p, err := change.DecodeTrackerWritePayload(fx.Payload)
	if err != nil {
		return failf(err, "tracker_write effect %s: payload refused", fx.Key)
	}
	if err := checkTarget(fx, p.Ticket); err != nil {
		return err
	}
	if p.Comment != "" {
		if err := t.deps.Writer.Comment(ctx, p.Ticket, fx.Key, p.Comment); err != nil {
			return failf(err, "tracker_write %s: comment", p.Ticket)
		}
	}
	if p.Transition != "" && t.deps.Transitioner != nil {
		if err := t.deps.Transitioner.Transition(ctx, p.Ticket, fx.Key, p.Transition); err != nil {
			return failf(err, "tracker_write %s: transition", p.Ticket)
		}
	}
	payload := change.TrackerUpdated{Ticket: p.Ticket, Stage: p.Stage, Transition: p.Transition}
	ev, err := newFact(t.deps.IDs, t.deps.Clock, change.EventTrackerUpdated,
		change.Actor{Kind: change.ActorJira, ID: trackerActorID}, payload)
	if err != nil {
		return failf(err, "tracker_write %s: build event", p.Ticket)
	}
	if err := t.deps.Ingest.Ingest(ctx, p.Ticket, []change.Event{ev}); err != nil {
		return failf(err, "tracker_write %s: ingest tracker.updated", p.Ticket)
	}
	return nil
}
