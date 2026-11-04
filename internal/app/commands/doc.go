// Package commands parses the room and GitHub command grammar into typed
// commands, and decides what to reply to a command that does not parse.
//
// # Purpose
//
// A human decides in the change room with `heimdall uphold F3`, or on a PR
// with `/heimdall uphold F3` (spec 11.5, D47, D139). This package turns that
// text into a Command or a reply. It does not act on the command, store it, or
// post the reply. The caller does those, through the event store and the chat port.
//
// # API
//
//   - Parse(Input, People) Result: the single entry point. Pure: same inputs,
//     same Result. It never returns an error. Each outcome is a Kind:
//     KindNotCommand (ignore), KindCommand (Command set), KindUnknown (grammar
//     reply), KindBadArgs (reason and usage reply), KindUnmapped (link reply).
//   - People: the interface the caller provides to map a platform sender id to a
//     person name. Defined here because this package is the consumer.
//   - Command: the sealed set of typed commands is the concrete types in
//     types.go. Callers type-switch on them.
//
// # Invariants
//
//   - A command counts only when its sender maps to a person (D140). Parse
//     checks the sender before it runs any grammar, so an agent or an unknown
//     account never reaches a verb's parser.
//   - Chat commands start with the plain word `heimdall`, case-insensitively, and
//     the word must end at a space or at the end of the text. GitHub comments
//     start with `/heimdall` under the same rule. A `!` or `@` form is never a command.
//   - A finding is always F followed by a number with no leading zero (F3,
//     F12), never a ULID. A ticket is an upper-case key and a number. Case and
//     memory refs are letters, digits, `_` and `-`.
//   - A dismiss reason is at least 15 runes, counted as runes, not bytes.
//   - Free-text values (remember, spec-change, incident, the evidence of a
//     challenge) keep their line breaks and are otherwise kept as typed.
//   - Replies never echo the sender's text. They show a reason and the usage
//     line with the source prefix, so a reply cannot be used to post arbitrary text.
//
// # Failure modes
//
//   - A nil People refuses every sender (KindUnmapped), so a missing mapping
//     fails closed.
//   - Unknown verbs get the grammar; a known verb with bad arguments gets its
//     reason and usage. Neither changes state. The caller posts the Reply and stops.
//   - Parse does no I/O and reads no clock, so it cannot block or fail for a
//     platform reason. A slow or failing People lookup is the caller's to bound.
//
// # Tests
//
// parse_test.go holds the table tests for every verb, the refusals (plain
// text, `!` and `/` in chat, mid-sentence mentions, quoted and fenced text),
// the unmapped and nil-people cases, the bad-argument cases with their reasons,
// rune counting for dismiss, and the reply length limit. `make fast` runs them.
// The room behaviour, meaning the approver's `heimdall uphold` gets through
// and an agent's post is refused, is board step P4.3, which is `not verified`
// until the Mattermost bridge (T26) and the decision path (T37) run.
package commands
