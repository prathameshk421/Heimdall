package main

import (
	"context"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/collaboration"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The board keeps proposals as facts in the change's log (AGENT_INTERACTION_ARCH 5.3); commandRuntime is their store.

func (r *commandRuntime) Events(ctx context.Context, target collaboration.Target) ([]change.Event, error) {
	return r.events.Load(ctx, target.ChangeID)
}

func (r *commandRuntime) Head(ctx context.Context, target collaboration.Target) (id.SHA, error) {
	events, err := r.events.Load(ctx, target.ChangeID)
	if err != nil {
		return "", err
	}
	v, err := change.Fold(events)
	if err != nil || v.PR == nil {
		return "", err
	}
	return v.PR.HeadSHA, nil
}

func (r *commandRuntime) Hold(ctx context.Context, target collaboration.Target, p change.CommandProposed) error {
	return r.appendBoard(ctx, target.ChangeID, change.EventCommandProposed, p, string(p.Trigger))
}

func (r *commandRuntime) Confirm(ctx context.Context, target collaboration.Target, c change.CommandConfirmed) error {
	return r.appendBoard(ctx, target.ChangeID, change.EventCommandConfirmed, c, string(c.MessageID))
}

// appendBoard appends one fact written by the board itself.
func (r *commandRuntime) appendBoard(ctx context.Context, cid id.ChangeID, typ change.EventType, payload change.Payload, ref string) error {
	existing, err := r.events.Load(ctx, cid)
	if err != nil {
		return err
	}
	ev, err := r.event(cid, change.Actor{Kind: change.ActorSystem, ID: "board"}, commands.SourceChat, typ, payload, ref)
	if err != nil {
		return err
	}
	_, err = r.events.Append(ctx, cid, int64(len(existing)), []change.Event{ev}, nil)
	return err
}

// ProposeCommand is propose_command for an agent working on ticket. The trigger message is read from the room by the
// board, so the sender and the text come from Switch, never from the agent's arguments.
func (r *commandRuntime) ProposeCommand(ctx context.Context, agent id.AgentName, ticket id.ChangeID, text string, trigger change.MessageID) (string, error) {
	events, err := r.events.Load(ctx, ticket)
	if err != nil {
		return "", fmt.Errorf("load %s: %w", ticket, err)
	}
	v, err := change.Fold(events)
	if err != nil {
		return "", err
	}
	if v.Room == "" {
		return "", fmt.Errorf("%w: %s has no room", collaboration.ErrRefused, ticket)
	}
	msg, err := r.findMessage(ctx, v.Room, trigger)
	if err != nil {
		return "", err
	}
	target := collaboration.Target{Room: ports.RoomBinding{RoomID: v.Room, ChangeID: ticket}, ChangeID: ticket}
	return r.processor.Propose(ctx, target, agent, text, msg, r.clock.Now())
}

// findMessage reads the room from the start and returns the message with the given id.
func (r *commandRuntime) findMessage(ctx context.Context, room change.RoomID, want change.MessageID) (ports.Message, error) {
	var cursor ports.Cursor
	for range 50 { // a room is read in pages; 50 pages is far beyond the recent window a trigger must be in
		msgs, next, err := r.chat.Read(ctx, room, cursor)
		if err != nil {
			return ports.Message{}, fmt.Errorf("read room %s: %w", room, err)
		}
		for _, m := range msgs {
			if m.ID == want {
				return m, nil
			}
		}
		if len(msgs) == 0 || next == cursor {
			break
		}
		cursor = next
	}
	return ports.Message{}, fmt.Errorf("%w: no message %s in the room", collaboration.ErrRefused, want)
}

var _ collaboration.Proposals = (*commandRuntime)(nil)
