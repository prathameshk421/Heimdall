package collaboration

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Proposals stores and reads the held-command facts of a change (AGENT_INTERACTION_ARCH 5.3, H10).
type Proposals interface {
	Events(ctx context.Context, target Target) ([]change.Event, error)
	Head(ctx context.Context, target Target) (id.SHA, error)
	Hold(ctx context.Context, target Target, p change.CommandProposed) error
	Confirm(ctx context.Context, target Target, c change.CommandConfirmed) error
}

// ErrRefused is a proposal the board will not run or hold. The agent gets the reason; nothing changed.
var ErrRefused = errors.New("refused")

func refuse(why string) error { return fmt.Errorf("%w: %s", ErrRefused, why) }

// Propose runs a command an agent asked for on behalf of the person who sent trigger (5.3). The person, read from the
// board's own copy of the room, is the actor; the agent only carries the words. The command is parsed and authorized
// exactly as if that person had typed it. A light verb runs now; a deciding verb is held and echoed until the same
// person answers yes on the same head. Text with no trigger from a mapped person can never become a command.
func (p *Processor) Propose(ctx context.Context, target Target, agent id.AgentName, text string, trigger ports.Message, now time.Time) (string, error) {
	if p.proposals == nil {
		return "", errors.New("collaboration: proposals are not configured")
	}
	if trigger.Room != target.Room.RoomID {
		return "", refuse("the trigger message is not in this change's room")
	}
	if now.Sub(trigger.At) > commands.ProposalTTL {
		return "", refuse("the trigger message is too old")
	}
	in := Input{Command: commands.Input{Text: "heimdall " + text, Source: commands.SourceChat, Sender: string(trigger.Sender)},
		Target: target, SourceRef: string(trigger.ID)}
	cmd, verb, person, err := p.authorizeAs(ctx, in)
	if err != nil {
		return "", err
	}
	if commands.Classes()[verb] != commands.ClassDeciding {
		return p.run(ctx, in, person, "proposed:", cmd, "Ran for "+person+": `heimdall "+text+"`")
	}
	head, err := p.proposals.Head(ctx, target)
	if err != nil {
		return "", fmt.Errorf("read head for proposal: %w", err)
	}
	held := change.CommandProposed{Trigger: trigger.ID, Sender: trigger.Sender, Person: change.ActorID(person), Text: text,
		Verb: verb, ProposedBy: agent, HeadSHA: head}
	if err := p.proposals.Hold(ctx, target, held); err != nil {
		return "", fmt.Errorf("hold proposal: %w", err)
	}
	echo := "I'll run: `heimdall " + text + "`. Reply **yes** to confirm."
	if err := p.replies.Reply(ctx, target, commands.SourceChat, string(trigger.ID), echo); err != nil {
		return "", err
	}
	return "held: waiting for " + person + " to reply yes", nil
}

// confirm runs the open proposal of the sender of a yes. A yes with nothing open does nothing.
func (p *Processor) confirm(ctx context.Context, in Input, now time.Time) error {
	if p.proposals == nil {
		return nil
	}
	events, err := p.proposals.Events(ctx, in.Target)
	if err != nil {
		return fmt.Errorf("read proposals: %w", err)
	}
	head, err := p.proposals.Head(ctx, in.Target)
	if err != nil {
		return fmt.Errorf("read head for confirmation: %w", err)
	}
	held, ok := commands.OpenProposal(events, change.SenderID(in.Command.Sender), head, now)
	if !ok {
		return nil
	}
	// Authorize again: the yes is the moment the person acts, and the policy may have changed since the echo.
	again := Input{Command: commands.Input{Text: "heimdall " + held.Text, Source: commands.SourceChat, Sender: in.Command.Sender},
		Target: in.Target, SourceRef: string(held.Trigger)}
	cmd, _, person, err := p.authorizeAs(ctx, again)
	if err != nil {
		return p.replies.Reply(ctx, in.Target, commands.SourceChat, in.SourceRef, "Not run: "+err.Error())
	}
	if err := p.proposals.Confirm(ctx, in.Target, change.CommandConfirmed{Trigger: held.Trigger, Sender: held.Sender,
		MessageID: change.MessageID(in.SourceRef)}); err != nil {
		return fmt.Errorf("confirm proposal: %w", err)
	}
	_, err = p.run(ctx, again, person, "confirmed:", cmd, "")
	return err
}

// authorizeAs parses and authorizes a command as the sender of in, refusing agents and unmapped senders.
func (p *Processor) authorizeAs(ctx context.Context, in Input) (commands.Command, string, string, error) {
	isAgent, err := p.identity.IsAgent(ctx, in.Target, commands.SourceChat, in.Command.Sender)
	if err != nil {
		return nil, "", "", fmt.Errorf("resolve sender kind: %w", err)
	}
	if isAgent {
		return nil, "", "", refuse("the trigger message was sent by an agent; only a person's own message can become a command")
	}
	people, err := p.identity.People(ctx, in.Target)
	if err != nil {
		return nil, "", "", fmt.Errorf("load people: %w", err)
	}
	parsed := commands.Parse(in.Command, people)
	if parsed.Kind != commands.KindCommand {
		return nil, "", "", refuse("not a command the sender can run: " + parsed.Reply)
	}
	person, err := p.identity.Authorize(ctx, in.Target, commands.SourceChat, in.Command.Sender, parsed.Command)
	if err != nil {
		return nil, "", "", refuse("the sender is not allowed to run " + parsed.Verb)
	}
	return parsed.Command, parsed.Verb, person, nil
}

// run executes an authorized command once (the source ref keeps it idempotent) and posts what ran.
func (p *Processor) run(ctx context.Context, in Input, person, refPrefix string, cmd commands.Command, ran string) (string, error) {
	msg, err := p.executor.Execute(ctx, in.Target, person, commands.SourceChat, refPrefix+in.SourceRef, cmd)
	if err != nil {
		return "", fmt.Errorf("execute %s: %w", cmd.Verb(), err)
	}
	if ran == "" {
		ran = msg
	}
	if ran == "" {
		ran = "Done."
	}
	if err := p.replies.Reply(ctx, in.Target, commands.SourceChat, in.SourceRef, ran); err != nil {
		return "", err
	}
	return ran, nil
}
