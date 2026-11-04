// Package trigger turns Jira ticket changes into change.triggered events (TECH_SPEC 3.2, 13, D50, D97).
//
// # Purpose
//
// A ticket that moves to In Progress with a human assignee starts a change. This package decides that. The Jira
// adapter (internal/adapters/tracker/jira) reports what changed. It does not decide what a trigger means.
//
// # API
//
//   - Config and New(Config) (*Poller, error): the ports, plus two small interfaces this package owns, CursorStore and
//     Projects. New refuses a nil port and names each one.
//   - Poller.PollOnce(ctx) (PollResult, error): one poll. The caller schedules it. The package has no loop, no ticker
//     and no sleep.
//   - PollResult: the changes read, the events appended (Triggered), the changes whose event was already there
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
// A change that passes every check appends one change.triggered event. Its payload names the ticket, the repo, the
// assignee and the source jira. Its actor is the jira actor. Its version is 1 in a new stream.
//
// # Dedupe
//
// The append uses expected version 0. A stream that already has events refuses it with ports.ErrVersionConflict. This
// package counts that as AlreadyTriggered and appends nothing. The event store is the durable dedupe: a repeat in the
// next poll, an overlap repeat, a restart and a second poller all end the same way.
//
// # Cursor and pending changes
//
// PollOnce loads the cursor and reads the changes after it. It handles each change in order and saves the reader's new
// cursor only when every change is handled: appended, already triggered, or ignored.
//
// A transport error (ports.ErrUnavailable) returns the error. A permanent error on one change returns an error that
// names the ticket key and changelog id. In both cases the cursor is not saved.
//
// The Jira adapter records each change as seen when it returns it. A later poll therefore does not return that change
// again, even though the cursor did not move. So the failed change and the rest of its poll are kept in memory
// (pending), and the next PollOnce handles them before it reads again. A restart drops pending. The cursor still
// points before those changes, so the first poll after the restart returns them again.
//
// # Known gaps
//
//   - The status is the current status. ports.TicketChange carries field names, not the status a move ended at. A
//     ticket that moves to In Progress and on again before the poll is ignored.
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
//   - An event is appended only after change.Event.Validate and change.Reduce accept it.
//   - PollOnce is not safe for concurrent calls on one Poller. Two Pollers may share the stores; the event store keeps
//     a single change.triggered per ticket.
//
// # Failure modes
//
// A port error is wrapped and returned, and the cursor stays where it was. An ErrVersionConflict from the event store
// is not an error. A cursor store that cannot save returns an error after the changes were handled. The next poll then
// reads the same window again. The event store makes each repeat a no-op, so the changes count as AlreadyTriggered.
//
// # Tests
//
// trigger_test.go runs PollOnce against in-memory fakes of TicketReader, EventStore, CursorStore, Projects, Clock and
// IDs. The fakes keep the store's version rule: Append refuses unless expected is the stream's last version. The tests
// cover each reason, the one-event rule for a valid change, repeats, a restart, two racing pollers, the cursor on
// transport and permanent errors, the pending retry, and the counts in PollResult.
package trigger
