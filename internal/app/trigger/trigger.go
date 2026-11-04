package trigger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Reason says why a change did not trigger. Each ignored change has exactly one reason.
type Reason string

// The reasons a change is ignored. They are checked in this order (doc.go, The rule).
const (
	ReasonNoStatusChange  Reason = "no_status_change"
	ReasonUnmappedProject Reason = "unmapped_project"
	ReasonTicketNotFound  Reason = "ticket_not_found"
	ReasonNotInProgress   Reason = "not_in_progress"
	ReasonUnassigned      Reason = "unassigned"
)

// statusField is the changelog field a status move names.
const statusField = "status"

// statusInProgress is the Jira status that starts a change (TECH_SPEC 13).
const statusInProgress = "In Progress"

// triggerActor is the id of the Jira actor that the trigger event names.
const triggerActor change.ActorID = "jira"

// triggerVersion is the version of the first event of a change. Starter takes the event at version 1.
const triggerVersion = 1

// Starter opens a change with its first event. Consumer: this package. The engine's Start stores the event and the
// steps' events in one append, and returns an error that wraps ports.ErrVersionConflict when the change already
// exists. The wiring adapts engine.Engine.Start to this signature, since Start also returns an engine.Outcome.
type Starter interface {
	Start(ctx context.Context, first change.Event) error
}

// CursorStore keeps the poll cursor of one tracker source. A store with no cursor yet returns the empty cursor and no
// error, so the first poll starts one overlap window back (ports.TicketReader, TECH_SPEC 13).
type CursorStore interface {
	Load(ctx context.Context) (ports.Cursor, error)
	Save(ctx context.Context, c ports.Cursor) error
}

// Projects maps a tracker project key, such as KAN, to the repo its tickets are built in (TECH_SPEC 2.6, D147).
type Projects interface {
	RepoFor(project string) (change.Repo, bool)
}

// Config holds the ports and stores one Poller needs. Every field is required.
type Config struct {
	Tickets  ports.TicketReader
	Starter  Starter
	Cursors  CursorStore
	Projects Projects
	Clock    ports.Clock
	IDs      ports.IDs
}

// Poller reads ticket changes and starts each triggered change. Build it with New. One goroutine calls PollOnce at a
// time.
type Poller struct {
	tickets  ports.TicketReader
	starter  Starter
	cursors  CursorStore
	projects Projects
	clock    ports.Clock
	ids      ports.IDs
	pending  []ports.TicketChange
}

// PollResult counts what one PollOnce call did. Ignored has an entry for every reason that occurred.
type PollResult struct {
	Changes          int
	Triggered        int
	AlreadyTriggered int
	Ignored          map[Reason]int
}

// New checks the configuration and builds a Poller. It refuses a nil port, naming each one.
func New(cfg Config) (*Poller, error) {
	var missing []string
	if cfg.Tickets == nil {
		missing = append(missing, "Tickets")
	}
	if cfg.Starter == nil {
		missing = append(missing, "Starter")
	}
	if cfg.Cursors == nil {
		missing = append(missing, "Cursors")
	}
	if cfg.Projects == nil {
		missing = append(missing, "Projects")
	}
	if cfg.Clock == nil {
		missing = append(missing, "Clock")
	}
	if cfg.IDs == nil {
		missing = append(missing, "IDs")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("trigger: nil port(s): %s", strings.Join(missing, ", "))
	}
	return &Poller{
		tickets:  cfg.Tickets,
		starter:  cfg.Starter,
		cursors:  cfg.Cursors,
		projects: cfg.Projects,
		clock:    cfg.Clock,
		ids:      cfg.IDs,
	}, nil
}

// PollOnce runs one poll. It handles the changes left over from a failed poll first, then reads the changes after the
// stored cursor, then saves the reader's new cursor. The cursor moves only when every change is handled.
func (p *Poller) PollOnce(ctx context.Context) (PollResult, error) {
	res := PollResult{Ignored: map[Reason]int{}}
	cursor, err := p.cursors.Load(ctx)
	if err != nil {
		return res, fmt.Errorf("load poll cursor: %w", err)
	}
	if err := p.drain(ctx, &res); err != nil {
		return res, err
	}
	changes, next, err := p.tickets.Changes(ctx, cursor)
	if err != nil {
		return res, fmt.Errorf("read ticket changes: %w", err)
	}
	res.Changes = len(changes)
	p.pending = changes
	if err := p.drain(ctx, &res); err != nil {
		return res, err
	}
	if err := p.cursors.Save(ctx, next); err != nil {
		return res, fmt.Errorf("save poll cursor: %w", err)
	}
	return res, nil
}

// drain handles the pending changes in order. A change leaves pending only after it is handled, so a change that
// failed stays first in line for the next poll (doc.go, Cursor and pending changes).
func (p *Poller) drain(ctx context.Context, res *PollResult) error {
	for len(p.pending) > 0 {
		ch := p.pending[0]
		if err := p.handle(ctx, ch, res); err != nil {
			return fmt.Errorf("trigger %s changelog %s: %w", ch.Key, ch.ChangelogID, err)
		}
		p.pending = p.pending[1:]
	}
	return nil
}

// handle decides one change and counts the outcome. It returns an error only for a failure that the next poll must
// retry or that needs a person to look at.
func (p *Poller) handle(ctx context.Context, ch ports.TicketChange, res *PollResult) error {
	if err := ch.Validate(); err != nil {
		return err
	}
	if !slices.Contains(ch.Fields, statusField) {
		res.Ignored[ReasonNoStatusChange]++
		return nil
	}
	repo, ok := p.projects.RepoFor(projectOf(ch.Key))
	if !ok {
		res.Ignored[ReasonUnmappedProject]++
		return nil
	}
	tk, err := p.tickets.Ticket(ctx, ch.Key)
	if errors.Is(err, ports.ErrNotFound) {
		res.Ignored[ReasonTicketNotFound]++
		return nil
	}
	if err != nil {
		return fmt.Errorf("read ticket: %w", err)
	}
	if tk.Status != statusInProgress {
		res.Ignored[ReasonNotInProgress]++
		return nil
	}
	if tk.Assignee == "" {
		res.Ignored[ReasonUnassigned]++
		return nil
	}
	started, err := p.startTriggered(ctx, ch.Key, repo, tk.Assignee)
	if err != nil {
		return err
	}
	if started {
		res.Triggered++
	} else {
		res.AlreadyTriggered++
	}
	return nil
}

// startTriggered starts the change with change.triggered. A change that already exists refuses the start with
// ports.ErrVersionConflict, which is the durable dedupe (TECH_SPEC 2.4). It reports false in that case, and it does
// not retry.
func (p *Poller) startTriggered(ctx context.Context, key id.ChangeID, repo change.Repo, assignee change.ActorID) (bool, error) {
	ev, err := p.triggeredEvent(key, repo, assignee)
	if err != nil {
		return false, err
	}
	err = p.starter.Start(ctx, ev)
	switch {
	case errors.Is(err, ports.ErrVersionConflict):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("start change.triggered: %w", err)
	}
	return true, nil
}

// triggeredEvent builds the change.triggered event of a ticket. The payload and the envelope are validated, and the
// reducer must accept the event from the empty state, as internal/app/intake checks its events.
func (p *Poller) triggeredEvent(key id.ChangeID, repo change.Repo, assignee change.ActorID) (change.Event, error) {
	pl := change.Triggered{Ticket: key, Repo: repo, Assignee: assignee, Source: change.TriggerJira}
	if err := pl.Validate(); err != nil {
		return change.Event{}, fmt.Errorf("payload of change.triggered: %w", err)
	}
	raw, err := json.Marshal(pl)
	if err != nil {
		return change.Event{}, fmt.Errorf("encode change.triggered: %w", err)
	}
	eid := p.ids.NewEvent()
	ev := change.Event{
		ID:            eid,
		ChangeID:      key,
		Version:       triggerVersion,
		Type:          change.EventChangeTriggered,
		Actor:         change.Actor{Kind: change.ActorJira, ID: triggerActor},
		OccurredAt:    p.clock.Now().UTC(),
		CorrelationID: change.CorrelationID(eid.String()),
		Payload:       raw,
	}
	if err := ev.Validate(); err != nil {
		return change.Event{}, fmt.Errorf("change.triggered for %s: %w", key, err)
	}
	if _, err := change.Reduce("", ev); err != nil {
		return change.Event{}, fmt.Errorf("change.triggered for %s: %w", key, err)
	}
	return ev, nil
}

// projectOf returns the project key of a ticket key: KAN for KAN-1. Project keys hold no hyphen, so the first one ends
// the project.
func projectOf(key id.ChangeID) string {
	project, _, _ := strings.Cut(string(key), "-")
	return project
}
