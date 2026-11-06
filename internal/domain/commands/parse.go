package commands

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// People maps a platform sender to a person name. The match is whole and
// exact: the sender id is not trimmed or case-folded (D140). A nil People
// refuses every sender.
type People interface {
	Lookup(src Source, sender string) (string, bool)
}

// unmappedReply tells a person how to get linked. It never shows the grammar,
// so a sender with no person learns nothing about what it could do.
const unmappedReply = "I do not know who you are yet, so this command was ignored. " +
	"Ask an admin to link your account to a person in Heimdall's people list."

// errUnknownVerb marks a command word that Heimdall does not know. Parse
// turns it into KindUnknown and the grammar reply.
var errUnknownVerb = errors.New("unknown command")

// Parse classifies one post. It is a pure function of its inputs: the same
// text and sender always give the same Result, with no I/O or clock.
//
// The order matters. Text that is not addressed to Heimdall is ignored before
// the sender is looked up, and the sender is checked before any grammar runs,
// so an unmapped sender never gets the grammar in a reply.
func Parse(in Input, people People) Result {
	body, ok := commandBody(in)
	if !ok {
		return Result{Kind: KindNotCommand}
	}
	person, mapped := lookupPerson(in, people)
	if !mapped {
		return Result{Kind: KindUnmapped, Reply: unmappedReply}
	}
	verb, args := cutWord(body)
	verb = strings.ToLower(verb)
	cmd, err := parseCommand(in.Source, verb, args)
	switch {
	case err == nil:
		return Result{Kind: KindCommand, Command: cmd, Verb: cmd.Verb(), Person: person}
	case errors.Is(err, errUnknownVerb):
		return Result{Kind: KindUnknown, Verb: verb, Person: person, Reply: grammar(in.Source)}
	default:
		return Result{Kind: KindBadArgs, Verb: verb, Person: person, Reply: err.Error()}
	}
}

// prefixOf returns the word that makes a post a command on a source. Chat
// uses the plain word, because Switch and the platforms take `!` and `/`
// posts as their own (D139). GitHub keeps `/heimdall`.
func prefixOf(src Source) (string, bool) {
	switch src {
	case SourceChat:
		return "heimdall", true
	case SourceGitHub:
		return "/heimdall", true
	default:
		return "", false
	}
}

// commandBody returns the text after the command word. The prefix matches
// case-insensitively and must end at a space or at the end of the text, so
// `heimdallx` and `heimdall-uphold` are not commands.
func commandBody(in Input) (string, bool) {
	prefix, ok := prefixOf(in.Source)
	if !ok {
		return "", false
	}
	text := strings.TrimLeftFunc(in.Text, unicode.IsSpace)
	if len(text) < len(prefix) || !strings.EqualFold(text[:len(prefix)], prefix) {
		return "", false
	}
	rest := text[len(prefix):]
	if rest != "" {
		r, _ := utf8.DecodeRuneInString(rest)
		if !unicode.IsSpace(r) {
			return "", false
		}
	}
	return rest, true
}

// lookupPerson resolves the sender through the people map. An empty sender or
// an empty name counts as unmapped.
func lookupPerson(in Input, people People) (string, bool) {
	if people == nil || in.Sender == "" {
		return "", false
	}
	name, ok := people.Lookup(in.Source, in.Sender)
	return name, ok && name != ""
}

// parseCommand dispatches on the lower-cased verb. Each verb has its own parser
// in verbs.go. The table is built per call, so there is no package-level state.
func parseCommand(src Source, verb, args string) (Command, error) {
	p, _ := prefixOf(src)
	parsers := map[string]func(p, args string) (Command, error){
		"uphold":            command(parseUphold),
		"dismiss":           command(parseDismiss),
		"start":             command(parseStart),
		"review":            command(parseReview),
		"approve-tests":     approveTests,
		"approve-profile":   command(parseApproveProfile),
		"approve-knowledge": command(parseApproveKnowledge),
		"promote-profile":   command(parsePromoteProfile),
		"challenge":         command(parseChallenge),
		"remember":          command(parseRemember),
		"forget":            command(parseForget),
		"spec-change":       command(parseSpecChange),
		"reject":            command(parseReject),
		"override":          command(parseOverride),
		"waive-reviewer":    command(parseWaiveReviewer),
		"incident":          command(parseIncident),
		"resume":            command(parseResume),
	}
	parse, ok := parsers[verb]
	if !ok {
		return nil, errUnknownVerb
	}
	return parse(p, args)
}

// command adapts a typed verb parser to the Command interface. On error the
// returned Command is not used, so its zero value does not matter.
func command[T Command](parse func(p, args string) (T, error)) func(p, args string) (Command, error) {
	return func(p, args string) (Command, error) {
		return parse(p, args)
	}
}

// approveTests adapts the no-argument verb. It is separate because it has no
// typed fields to parse.
func approveTests(p, args string) (Command, error) {
	if err := checkApproveTests(p, args); err != nil {
		return nil, err
	}
	return ApproveTests{}, nil
}

// grammar is the one short reply for an unknown command. It lists every verb
// with the prefix for the source, so a GitHub user sees `/heimdall` and a chat
// user sees `heimdall`.
func grammar(src Source) string {
	p, _ := prefixOf(src)
	uses := []string{
		useUphold, useDismiss, useReject, useOverride, useWaive, useReview,
		useApprove, useApproveProfile, useApproveKnowledge, usePromoteProfile, useStart, useChallenge, useRemember, useForget, useSpec, useIncident, useResume,
	}
	for i, u := range uses {
		uses[i] = p + " " + u
	}
	return "Not a command I know. Try: " + strings.Join(uses, "; ") + "."
}
