// Package collaboration connects external human messages to the command grammar and action boundary.
package collaboration

import (
	"context"
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Target locates the conversation that received a command. Chat messages have RoomBinding; GitHub commands have repo
// and pull-request number. ChangeID is the bound change for either source when one exists.
type Target struct {
	Room     ports.RoomBinding
	ChangeID id.ChangeID
	Repo     change.Repo
	PR       int
}

// Input preserves source metadata required to refuse edited comments and bot comments before parsing.
type Input struct {
	Command   commands.Input
	Target    Target
	SourceRef string
	Edited    bool
	Bot       bool
}

// Identity resolves chat senders and GitHub logins against the policy at the change's base revision.
type Identity interface {
	People(ctx context.Context, target Target) (commands.People, error)
	IsAgent(ctx context.Context, target Target, source commands.Source, sender string) (bool, error)
	Authorize(ctx context.Context, target Target, source commands.Source, sender string, command commands.Command) (string, error)
}

// Executor performs an authorized command action. Implementations append idempotent domain events or use their owning
// application services; untrusted command text must remain data, never an instruction to an agent.
type Executor interface {
	Execute(ctx context.Context, target Target, person string, source commands.Source, sourceRef string, command commands.Command) (string, error)
}

// Replies writes a source-specific, idempotent reply (Switch post or GitHub issue comment).
type Replies interface {
	Reply(ctx context.Context, target Target, source commands.Source, sourceRef, body string) error
}

// Config contains the four collaborators for command processing.
type Config struct {
	Identity Identity
	Executor Executor
	Replies  Replies
}

// Processor parses, authorizes, dispatches and replies to one Switch message or GitHub issue comment.
type Processor struct {
	identity Identity
	executor Executor
	replies  Replies
}

// New validates collaborators and builds the command processor.
func New(cfg Config) (*Processor, error) {
	var missing []string
	if cfg.Identity == nil {
		missing = append(missing, "Identity")
	}
	if cfg.Executor == nil {
		missing = append(missing, "Executor")
	}
	if cfg.Replies == nil {
		missing = append(missing, "Replies")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("collaboration commands: missing %v", missing)
	}
	return &Processor{identity: cfg.Identity, executor: cfg.Executor, replies: cfg.Replies}, nil
}

// Handle processes one incoming message. Non-command text, edited GitHub comments, bots, and mapped agents never
// change state. Invalid or unauthorized human commands receive a bounded reply; executor errors are returned without
// echoing untrusted text or implementation details.
func (p *Processor) Handle(ctx context.Context, in Input) error {
	if in.Edited || in.Bot || in.SourceRef == "" {
		return nil
	}
	src := in.Command.Source
	return p.handleHuman(ctx, in, src)
}

func (p *Processor) handleHuman(ctx context.Context, in Input, src commands.Source) error {
	isAgent, err := p.identity.IsAgent(ctx, in.Target, src, in.Command.Sender)
	if err != nil {
		return fmt.Errorf("resolve command sender kind: %w", err)
	}
	if isAgent {
		return nil
	}
	people, err := p.identity.People(ctx, in.Target)
	if err != nil {
		return fmt.Errorf("load command people: %w", err)
	}
	parsed := commands.Parse(in.Command, people)
	return p.dispatch(ctx, in, src, parsed)
}

func (p *Processor) dispatch(ctx context.Context, in Input, src commands.Source, parsed commands.Result) error {
	switch parsed.Kind {
	case commands.KindNotCommand:
		return nil
	case commands.KindUnmapped, commands.KindUnknown, commands.KindBadArgs:
		return p.replies.Reply(ctx, in.Target, src, in.SourceRef, parsed.Reply)
	case commands.KindCommand:
	default:
		return errors.New("collaboration commands: parser returned unknown result kind")
	}
	person, err := p.identity.Authorize(ctx, in.Target, src, in.Command.Sender, parsed.Command)
	if err != nil {
		return p.replies.Reply(ctx, in.Target, src, in.SourceRef, "You are not authorized to run that command.")
	}
	message, err := p.executor.Execute(ctx, in.Target, person, src, in.SourceRef, parsed.Command)
	if err != nil {
		return fmt.Errorf("execute %s command: %w", parsed.Verb, err)
	}
	if message == "" {
		message = "Recorded."
	}
	return p.replies.Reply(ctx, in.Target, src, in.SourceRef, message)
}

// HandleRoomMessage is the roomstream adapter. The room-change binding is retained for policy and action resolution.
func (p *Processor) HandleRoomMessage(ctx context.Context, room ports.RoomBinding, message ports.Message) error {
	return p.Handle(ctx, Input{
		Command: commands.Input{Text: message.Text, Source: commands.SourceChat, Sender: string(message.Sender)},
		Target:  Target{Room: room, ChangeID: room.ChangeID}, SourceRef: string(message.ID),
	})
}
