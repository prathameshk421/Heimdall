// Package collaboration connects external human messages to the command grammar and action boundary.
package collaboration

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

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

// Questioner answers a mapped person's plain-words @heimdall question from the change's recorded facts (T1, U6).
type Questioner interface {
	AnswerQuestion(ctx context.Context, target Target, person, question string) (string, error)
}

// Config contains the collaborators for command processing. Proposals and Clock serve propose_command and the yes
// that confirms it (5.3); without them a yes is ordinary chat and Propose refuses.
type Config struct {
	Identity   Identity
	Executor   Executor
	Replies    Replies
	Proposals  Proposals
	Clock      ports.Clock
	Questioner Questioner
}

// Processor parses, authorizes, dispatches and replies to one Switch message or GitHub issue comment.
type Processor struct {
	identity   Identity
	executor   Executor
	replies    Replies
	proposals  Proposals
	clock      ports.Clock
	questioner Questioner
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
	if (cfg.Proposals == nil) != (cfg.Clock == nil) {
		return nil, errors.New("collaboration commands: Proposals and Clock come together")
	}
	return &Processor{identity: cfg.Identity, executor: cfg.Executor, replies: cfg.Replies, proposals: cfg.Proposals, clock: cfg.Clock, questioner: cfg.Questioner}, nil
}

// Handle processes one incoming message. Non-command text, edited GitHub comments, bots, and mapped agents never
// change state. Invalid or unauthorized human commands receive a bounded reply; executor errors are returned without
// echoing untrusted text or implementation details.
func (p *Processor) Handle(ctx context.Context, in Input) error {
	if in.Edited || in.Bot || in.SourceRef == "" {
		return nil
	}
	if in.Command.Source == commands.SourceChat && commands.IsYes(in.Command.Text) && p.proposals != nil {
		return p.confirm(ctx, in, p.clock.Now())
	}
	// Most room traffic is agent and human chatter. Decide that from the text alone, before any identity read,
	// so plain chat never spends a GitHub policy lookup.
	if !commands.IsCommand(in.Command) {
		if in.Command.Source == commands.SourceChat && p.questioner != nil {
			if q, ok := extractMentionQuestion(in.Command.Text); ok {
				return p.handleMention(ctx, in, q)
			}
		}
		return nil
	}
	src := in.Command.Source
	return p.handleHuman(ctx, in, src)
}

// handleMention answers a message that names the coordinator, whether it asks a question, asks for status or asks for
// the command help. The sender must be a mapped person, never an agent (T1).
func (p *Processor) handleMention(ctx context.Context, in Input, text string) error {
	person, ok, err := p.questionPerson(ctx, in)
	if err != nil || !ok {
		return err
	}
	if word := readOnlyRequest(text); word != "" {
		return p.replyReadOnly(ctx, in, person, word)
	}
	return p.answerQuestion(ctx, in, person, text)
}

// questionPerson resolves the sender of a question: an agent, or a sender with no person, gets no answer.
func (p *Processor) questionPerson(ctx context.Context, in Input) (string, bool, error) {
	isAgent, err := p.identity.IsAgent(ctx, in.Target, in.Command.Source, in.Command.Sender)
	if err != nil {
		return "", false, fmt.Errorf("resolve question sender kind: %w", err)
	}
	if isAgent {
		return "", false, nil
	}
	people, err := p.identity.People(ctx, in.Target)
	if err != nil {
		return "", false, fmt.Errorf("load question people: %w", err)
	}
	person, ok := people.Lookup(in.Command.Source, in.Command.Sender)
	if !ok || person == "" {
		return "", false, nil // unmapped senders never trigger @heimdall answers (T1)
	}
	return person, true, nil
}

func (p *Processor) answerQuestion(ctx context.Context, in Input, person, question string) error {
	answer, err := p.questioner.AnswerQuestion(ctx, in.Target, person, question)
	if err != nil {
		return fmt.Errorf("answer question: %w", err)
	}
	if strings.TrimSpace(answer) == "" {
		return nil
	}
	return p.replies.Reply(ctx, in.Target, in.Command.Source, in.SourceRef, answer)
}

// replyReadOnly answers a request that changes nothing: the command help, or the status of the change.
func (p *Processor) replyReadOnly(ctx context.Context, in Input, person, word string) error {
	if word == readOnlyHelp {
		return p.replies.Reply(ctx, in.Target, in.Command.Source, in.SourceRef, commands.Grammar(in.Command.Source))
	}
	return p.answerQuestion(ctx, in, person, word)
}

const (
	readOnlyHelp   = "help"
	readOnlyStatus = "status"
)

// readOnlyRequest says which read-only request the text is ("status", "help"), or "" for anything else. It accepts
// both the command form ("heimdall status") and the mention form ("@heimdall-coord-main status"), on any platform. A
// status may name a ticket after it; help is the whole request.
func readOnlyRequest(text string) string {
	fields := strings.Fields(commands.WithoutLeadingMention(strings.TrimSpace(text)))
	if len(fields) == 0 {
		return ""
	}
	trim := func(s string) string { return strings.Trim(s, "?:,.") }
	first := strings.ToLower(trim(fields[0]))
	if first == "heimdall" {
		if len(fields) < 2 {
			return ""
		}
		first = strings.ToLower(trim(fields[1]))
		fields = fields[1:]
	}
	if first == readOnlyHelp && len(fields) > 1 {
		return ""
	}
	if first == readOnlyStatus || first == readOnlyHelp {
		return first
	}
	return ""
}

// reCoordinatorMention matches a leading mention of the coordinator: @heimdall, @heimdall-coord-main, @Heimdall. Slack
// sends its own form, "<@U0123ABC>", which isMentionToken reads.
var reCoordinatorMention = regexp.MustCompile(`(?i)^@heimdall(?:[-_.][A-Za-z0-9_-]+)?\b[:,]?`)

// extractMentionQuestion returns the words after a leading mention of the coordinator. A message that mentions nobody,
// or mentions the coordinator and says nothing else, is not a question.
func extractMentionQuestion(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	mentioned := reCoordinatorMention.MatchString(trimmed) || isSlackMention(trimmed)
	if !mentioned {
		return "", false
	}
	rest := strings.TrimSpace(commands.WithoutLeadingMention(trimmed))
	if rest == "" || rest == trimmed {
		return "", false
	}
	return rest, true
}

// isSlackMention reports whether the text starts with a Slack user mention, "<@U0123ABC>" or "<@U0123ABC|name>".
func isSlackMention(text string) bool {
	if !strings.HasPrefix(text, "<@") {
		return false
	}
	end := strings.IndexByte(text, '>')
	return end > 2
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
	// Status and help change nothing, so they answer before the grammar runs. Uphold, dismiss, waive and every other
	// state-changing verb still goes through Parse and Authorize unchanged.
	if p.questioner != nil {
		if person, ok := people.Lookup(src, in.Command.Sender); ok && person != "" {
			if word := readOnlyRequest(in.Command.Text); word != "" {
				return p.replyReadOnly(ctx, in, person, word)
			}
		}
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
		return p.replies.Reply(ctx, in.Target, src, in.SourceRef, "Sorry, you are not allowed to run that command. Ask an approver for this repository.")
	}
	message, err := p.executor.Execute(ctx, in.Target, person, src, in.SourceRef, parsed.Command)
	if errors.Is(err, ErrRefused) {
		return p.replies.Reply(ctx, in.Target, src, in.SourceRef, refusalText(err))
	}
	if err != nil {
		return fmt.Errorf("execute %s command: %w", parsed.Verb, err)
	}
	if message == "" {
		message = "Done."
	}
	return p.replies.Reply(ctx, in.Target, src, in.SourceRef, message)
}

// refusalText is what the person reads when the executor refuses a command: the reason, without the error wrapper.
// A refusal is a decision, not a failure, so the poller must not retry it.
func refusalText(err error) string {
	why := strings.TrimSuffix(strings.TrimPrefix(err.Error(), ErrRefused.Error()+": "), ".")
	return "Not done: " + why + "."
}

// HandleRoomMessage is the roomstream adapter. The room-change binding is retained for policy and action resolution.
// A person may name the ticket in the message ("heimdall uphold KAN-14 F1"); the key is not part of the command, so
// it is taken out before the command is read.
func (p *Processor) HandleRoomMessage(ctx context.Context, room ports.RoomBinding, message ports.Message) error {
	return p.Handle(ctx, Input{
		Command: commands.Input{Text: withoutKey(message.Text, room.ChangeID), Source: commands.SourceChat, Sender: string(message.Sender)},
		Target:  Target{Room: room, ChangeID: room.ChangeID}, SourceRef: string(message.ID),
	})
}

// HandleSharedRoomMessage takes a message from a standing channel that serves several tickets. A command must name
// its ticket; without one, or with two, the sender is asked which one they mean. A mention question, `heimdall status`
// and `heimdall help` change nothing, so they are answered here too, the same way as in a ticket room.
func (p *Processor) HandleSharedRoomMessage(ctx context.Context, room change.RoomID, candidates []ports.RoomBinding, message ports.Message) error {
	in := commands.Input{Text: message.Text, Source: commands.SourceChat, Sender: string(message.Sender)}
	word := readOnlyRequest(message.Text)
	if word == "" && commands.IsCommand(in) {
		return p.handleSharedCommand(ctx, in, candidates, message)
	}
	return p.handleSharedQuestion(ctx, room, candidates, message)
}

// handleSharedCommand runs a command in a standing room: it needs its ticket key when several are open.
func (p *Processor) handleSharedCommand(ctx context.Context, in commands.Input, candidates []ports.RoomBinding, message ports.Message) error {
	var named []ports.RoomBinding
	for _, c := range candidates {
		if namesKey(message.Text, c.ChangeID) {
			named = append(named, c)
		}
	}
	switch {
	case len(named) == 0 && len(candidates) == 1:
		named = candidates // one open ticket here: it is the one meant
	case len(named) == 0 && len(candidates) > 0 && commands.RepositoryScoped(commands.VerbOf(in)):
		named = candidates[:1] // the proposal id names the repository; any binding carries the command
	}
	if len(named) != 1 {
		return p.askWhichTicket(ctx, candidates, message)
	}
	return p.HandleRoomMessage(ctx, named[0], message)
}

// askWhichTicket asks a sender which ticket a command means, naming each open change the way chat reads it.
func (p *Processor) askWhichTicket(ctx context.Context, candidates []ports.RoomBinding, message ports.Message) error {
	keys := make([]string, 0, len(candidates))
	for _, c := range candidates {
		keys = append(keys, c.ChangeID.ChatLabel())
	}
	ask := "Which ticket is that about? Put its key in the message, for example: heimdall uphold " + string(candidates[0].ChangeID) +
		" F1. Open tickets here: " + strings.Join(keys, ", ") + "."
	first := candidates[0]
	return p.replies.Reply(ctx, Target{Room: first, ChangeID: first.ChangeID}, commands.SourceChat, string(message.ID), ask)
}

// handleSharedQuestion answers a mention question, `heimdall status` or `heimdall help` in a standing channel. One open
// change is the one meant; several get a short question with readable names; none gets a plain "nothing open".
func (p *Processor) handleSharedQuestion(ctx context.Context, room change.RoomID, candidates []ports.RoomBinding, message ports.Message) error {
	if p.questioner == nil {
		return nil
	}
	text := strings.TrimSpace(message.Text)
	question, asked := extractMentionQuestion(text)
	word := readOnlyRequest(text)
	if !asked && word == "" {
		return nil
	}
	first := ports.RoomBinding{RoomID: room}
	if len(candidates) > 0 {
		first = candidates[0]
	}
	in := Input{
		Command:   commands.Input{Text: text, Source: commands.SourceChat, Sender: string(message.Sender)},
		Target:    Target{Room: first, ChangeID: first.ChangeID},
		SourceRef: string(message.ID),
	}
	person, ok, err := p.questionPerson(ctx, in)
	if err != nil || !ok {
		return err
	}
	if word == readOnlyHelp {
		return p.replies.Reply(ctx, in.Target, commands.SourceChat, in.SourceRef, commands.Grammar(commands.SourceChat))
	}
	return p.answerShared(ctx, in, person, word, question, candidates)
}

// askedText is what a standing-room question asks about: the status, or the words after the mention.
func askedText(word, question string) string {
	if word == readOnlyStatus {
		return readOnlyStatus
	}
	return question
}

// answerShared picks the change a standing-room question is about: one it names, the only one open, or a question back
// with readable names. A channel with no open change gets a plain "nothing open".
func (p *Processor) answerShared(ctx context.Context, in Input, person, word, question string, candidates []ports.RoomBinding) error {
	for _, c := range candidates {
		if namesKey(in.Command.Text, c.ChangeID) {
			return p.answerQuestion(ctx, aboutChange(in, c), person, askedText(word, question))
		}
	}
	switch len(candidates) {
	case 0:
		return p.replies.Reply(ctx, in.Target, commands.SourceChat, in.SourceRef,
			"There are no open pull requests or tickets here right now.")
	case 1:
		return p.answerQuestion(ctx, aboutChange(in, candidates[0]), person, askedText(word, question))
	}
	labels := make([]string, 0, len(candidates))
	for _, c := range candidates {
		labels = append(labels, c.ChangeID.ChatLabel())
	}
	return p.replies.Reply(ctx, in.Target, commands.SourceChat, in.SourceRef,
		"Which one do you mean? Open changes here: "+strings.Join(labels, ", ")+".")
}

// aboutChange points a question at one change of a standing room.
func aboutChange(in Input, c ports.RoomBinding) Input {
	return Input{Command: in.Command, Target: Target{Room: c, ChangeID: c.ChangeID}, SourceRef: in.SourceRef}
}

// keyPattern matches a ticket key as a word, in any letter case.
func keyPattern(key id.ChangeID) *regexp.Regexp {
	return regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(string(key)) + `\b`)
}

func namesKey(text string, key id.ChangeID) bool { return keyPattern(key).MatchString(text) }

// withoutKey removes the first mention of the ticket key from a command, so "heimdall uphold KAN-14 F1" reads as
// "heimdall uphold F1".
func withoutKey(text string, key id.ChangeID) string {
	re := keyPattern(key)
	loc := re.FindStringIndex(text)
	if loc == nil {
		return text
	}
	return strings.Join(strings.Fields(text[:loc[0]]+" "+text[loc[1]:]), " ")
}
