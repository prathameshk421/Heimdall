// Package trigger turns Jira ticket changes into change.triggered events (TECH_SPEC 3.2, 13, D50, D97).
//
// # Purpose
//
// A ticket that moves to In Progress with a human assignee starts a change. This package decides that. The Jira
// adapter (internal/adapters/tracker/jira) reports what changed. It does not decide what a trigger means.
//
// # API
//
//   - Config and New(Config) (*Poller, error): the ports, plus three small interfaces this package owns, Starter,
//     CursorStore and Projects. New refuses a nil port and names each one.
//   - Starter: Start(ctx, change.Event) error. The engine's Start opens a change with its first event. The wiring adapts
//     engine.Engine.Start to this signature, since Start also returns an engine.Outcome.
//   - Poller.PollOnce(ctx) (PollResult, error): one poll. The caller schedules it. The package has no loop, no ticker
//     and no sleep.
//   - PollResult: the changes read, the changes started (Triggered), the changes that already existed
//     (AlreadyTriggered), and the ignored changes counted by Reason.
//   - Reason: why a change did not trigger. Each ignored change has exactly one reason.
//
// # The rule
//
// A changelog entry (ports.TicketChange) triggers when every check below passes. The checks run in this order. The
// first check that fails sets the reason.
//
//  1. The entry names the field "status". Otherwise: no_status_change.
//  2. The ticket's project maps to a repo in Projects. Otherwise: unmapped_project. No ticket read is made.
//  3. TicketReader.Ticket returns the ticket. ErrNotFound: ticket_not_found.
//  4. The ticket's status is "In Progress". Otherwise: not_in_progress.
//  5. The ticket has an assignee. Otherwise: unassigned.
//
// A change that passes every check starts with one change.triggered event. Its payload names the ticket, the repo, the
// assignee and the source jira. Each ticket acceptance criterion is validated, stored in BlobEvidence under its
// SHA-256 ref, and captured in the event with its ordered ID, source text, hash and ref. The role bundle and task
// envelope contain only ID/ref pairs; MCP exposes the text only through read_untrusted. Its actor is the jira actor.
// Its version is 1.
//
// # Dedupe
//
// The event goes to Starter.Start. A change that already exists returns an error that wraps ports.ErrVersionConflict.
// This package counts that as AlreadyTriggered and starts nothing. The engine is the durable dedupe: a repeat in the
// next poll, an overlap repeat, a restart and a second poller all end the same way. The start does not retry.
//
// # Cursor and pending changes
//
// PollOnce loads the cursor and reads the changes after it. It handles each change in order and saves the reader's new
// cursor only when every change is handled: appended, already triggered, or ignored.
//
// A transport error (ports.ErrUnavailable) returns the error. A permanent error on one change returns an error that
// names the ticket key and changelog id. In both cases the cursor is not saved. A Starter error that is not a version
// conflict is permanent in this sense.
//
// The Jira adapter records each change as seen when it returns it. A later poll therefore does not return that change
// again, even though the cursor did not move. So the failed change and the rest of its poll are kept in memory
// (pending), and the next PollOnce handles them before it reads again. A restart drops pending. The cursor still
// points before those changes, so the first poll after the restart returns them again.
//
// # Known gaps
//
//   - A status move is judged on TicketChange.ToStatus, the status it ended at (A10 item 1). A file change has no move,
//     so it is judged on the current status. A file-tracker ticket's status is therefore still read at poll time.
//   - App accounts are not checked here. ports.Ticket has no account kind, so this package cannot tell an app account
//     from a person. The Jira adapter drops app-assigned changes before they reach this package. A ticket that is
//     reassigned to an app between the change read and the ticket read would still trigger. Closing this needs a
//     port field (see the report of T36).
//
// # Invariants
//
//   - Time comes from ports.Clock and ids from ports.IDs. There is no time.Now, no logging and no fmt.Print.
//   - An error names the ticket key, the changelog id, or the port error that was wrapped. This package never reads a
//     credential, so no credential value can reach an error.
//   - An event is started only after change.Event.Validate and change.Reduce accept it.
//   - PollOnce is not safe for concurrent calls on one Poller. Two Pollers may share the Starter; the engine keeps a
//     single change.triggered per ticket.
//
// # Failure modes
//
// A port error, invalid ticket or failed criterion-blob write is wrapped and returned, and the cursor stays where it
// was. Content-addressed writes are idempotent; a retry gets the same ref. An ErrVersionConflict from the Starter is
// not an error. A cursor store that cannot save returns an error after the changes were handled. The next poll then
// reads the same window again. The Starter refuses each repeat, so the changes count as AlreadyTriggered.
//
// # Tests
//
// trigger_test.go runs PollOnce against in-memory fakes of TicketReader, Starter, CursorStore, Projects, Clock and IDs.
// The fake Starter refuses a change that already has a stream with ports.ErrVersionConflict, as the engine does. The
// tests cover each reason, the one-event rule for a valid change, repeats, a restart, two racing pollers, the cursor on
// transport and permanent errors, the pending retry, and the counts in PollResult.
//
// Not verified here: the engine's Start with the real postgres store, and the wiring adapter from engine.Start to
// Starter. Both belong to T20 and the wiring task.
package trigger
