// Package effects performs the effects that steps decide: it asks the outside systems for a room and a branch, then
// records the result as an event (TECH_SPEC 2.4, 5.1; task T23). It imports the standard library, internal/domain/change,
// internal/domain/id and internal/ports only.
//
// Public API
//   - RoomOpen (NewRoomOpen, RoomOpenDeps): performs room_open. It reads the ticket title through ports.TicketReader,
//     calls ports.Chat.OpenRoom with the effect key as the request key, and ingests room.opened.
//   - GitHubBranch (NewGitHubBranch, GitHubBranchDeps): performs github_branch. It expands the template of the payload
//     with the ticket key and a slug of the title, calls ports.BranchWriter.EnsureBranch, and ingests branch.ensured.
//   - Ingest: the consumer-defined interface that hands executor events to the engine.
//   - BaseReader: reads the base commit of a default branch (see the gap below).
//   - Slug: the slug of a branch name (lower case ASCII letters and digits, hyphens, at most 40 characters).
//
// Each Execute follows one order. It checks the kind and the target, decodes the payload with the domain decoder, and
// loads the change. If the result event is already stored, it returns nil and does nothing else: a retry after a crash
// between the outside action and the ingest repeats nothing. Otherwise it performs the action and ingests one event.
// The event has the adapter as actor, the cause event (change.triggered for a room, room.opened for a branch) as
// causation, and the cause's correlation id. Version and hash stay empty: the engine numbers the event.
//
// Errors. Every error the executor returns has fixed text with ids. The text of a cause is never copied into the
// message, so an outbox row stores no secret (outbox.Executor). The cause stays reachable with errors.Is and errors.As.
//
// Gap. ports.BranchWriter.EnsureBranch takes no idempotency key, so the branch write does not carry the effect key.
// The port is idempotent by name (it does nothing when the branch exists), and the retry check above covers the rest.
// No port reads the default branch head, so BaseReader is a consumer interface that no adapter implements yet.
//
// Tests. The tests use fakes for every port and for Ingest, and check the effect key, the title, the event fields,
// the retry path, the error text with a canary, and the slug rule.
package effects
