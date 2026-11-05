// Package file is the file tracker adapter (ports.TicketReader, TECH_SPEC 13, D50). It is used by local mode and
// by tests, and reads seed/tickets/<KEY>.md.
//
// # Ticket file format
//
// The sample is testdata/repos/pay-demo/base/seed/tickets/PAY-142.md. The first non-blank line is
// "# KEY: title", where KEY must equal the file name. Between that heading and the next "#" heading, two lines
// set the owner and the state: "Assignee: <login>" and "Status: <name>". Both are required, because the ticket
// model requires them. The criteria come from the "Acceptance criteria" section (bullets or numbers, see
// internal/adapters/tracker/internal/criteria).
//
// # API
//
//   - New(dir, ports.Clock) (*Tracker, error).
//   - Tracker.Changes(ctx, since): the ticket files modified inside since minus poll.Overlap. The changelog id is
//     the SHA-256 of the file content, so a touch that keeps the content is not a change. Fields is always
//     ["file"], because a file does not say which field moved.
//   - Tracker.Ticket(ctx, key): one ticket. A missing file wraps ports.ErrNotFound.
//
// # Invariants
//
//   - A key must match ^[A-Z][A-Z0-9_]*-[0-9]+$, so no separator or dot reaches the file system (os.Root also
//     refuses escapes).
//   - Each (key, content digest) pair is reported at most once inside the window (poll.Seen).
//   - The directory is opened per call, so a file added later is seen.
//
// # Tests
//
// file_test.go: the sample format, the heading key check, the missing owner, path-like keys, the window, a repeat
// inside the overlap, an edit, and non-ticket files being ignored.
package file
