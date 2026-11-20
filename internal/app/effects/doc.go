// Package effects holds the executors of the jobs that rows start (TECH_SPEC 5.1, 5.2): each asks one outside system
// for something (a room, a branch, a commit, a sandbox run, a check) and records the result as one fact. It imports
// the standard library, the domain packages and internal/ports only.
//
// Public API
//   - RoomOpen (NewRoomOpen, RoomOpenDeps): performs room_open. It reads the ticket title through ports.TicketReader,
//     calls ports.Chat.OpenRoom with the effect key as the request key, and ingests room.opened.
//   - GitHubBranch (NewGitHubBranch, GitHubBranchDeps): performs github_branch. It expands the template of the payload
//     with the ticket key and a slug of the title, calls ports.BranchWriter.EnsureBranch, and ingests branch.ensured.
//   - The other executors, one per file: task_assign (the wake line), test_quality (red first, flake, seam),
//     acceptance, run_checks (checks.go), verify_evidence and github_check (the gate and other check runs).
//   - Ingest: the consumer-defined interface that hands executor events to the log (reconcile.Recorder).
//   - Slug: the slug of a branch name (lower case ASCII letters and digits, hyphens, at most 40 characters).
//
// Each Execute follows one order. It checks the kind and the target, decodes the payload with the domain decoder,
// performs the action and ingests one fact. It decides nothing and holds no "already done" check (H6): the payload
// carries everything it needs, and a fact recorded twice folds to the same view. A crash between the outside action
// and the ingest can repeat the action once on retry. The fact has the adapter as actor; the recorder sets the change
// id, the version, the causation and the correlation.
//
// Errors. Every error the executor returns has fixed text with ids. The text of a cause is never copied into the
// message, so an outbox row stores no secret (outbox.Executor). The cause stays reachable with errors.Is and errors.As.
//
// ports.BranchWriter.EnsureBranch takes the effect key (A10 item 3), so the branch write carries it. The port is
// idempotent by name (it does nothing when the branch exists), so a repeated branch job is harmless.
//
// Tests. The tests use fakes for every port and for Ingest, and check the effect key, the title, the event fields,
// the error text with a canary, and the slug rule.
package effects
