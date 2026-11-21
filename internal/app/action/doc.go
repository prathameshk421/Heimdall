// Package action holds the use cases of Action mode: the stateless gate run that decides the fast check and publishes
// it, and the ingest step that turns third-party review comments into candidate findings (ACTION_MODE 5, 6, work
// package AM14).
//
// # Purpose
//
// Action mode runs Heimdall as a step in the customer's GitHub Actions. There is no database and no reconciler: one
// gate run reads the state log on a git ref, reads GitHub, asks package actionflow what to do, does it, and appends
// what it learned. This package is that run. The rules are in the domain (fastcheck, testintegrity, external,
// actionflow, summary); this package only reads, decides which rule to call, and writes.
//
// The proof run is a later work package (AM14b). This package reads a proof result the log already holds and gives it
// to the summary, and it never starts or performs a proof run.
//
// # API
//
//   - NewIngester(comments) and Ingester.Ingest(ctx, IngestIn): the review comments of one pull request, read through
//     ports.PRReviewCommentReader and mapped by package external. It returns the change.ExternalIngested payloads to
//     record, the candidates that duplicate one of Heimdall's own findings, and the counts of what was dropped or
//     ignored.
//   - NewGate(Config), (*Gate).Run(ctx, changeID): one gate run.
//   - Result: the head, the fast verdict, the events appended, the checks posted, and whether the summary was written.
//   - ErrStateUnverified: the state log failed its MAC or its hash chain. Nothing is written to a log that failed.
//   - Config.Resetter starts a new log with state.reset (ACTION_MODE 2.7). A store that cannot reset leaves it nil, and
//     the gate then fails closed with ErrStateUnverified.
//   - Config.Facts reads the fast-check groups that no port covers: the secret scan, the rule cards at the merge base,
//     and the test-integrity rules. cmd wires it to internal/secretscan and the domain rules, because the dependency
//     rule keeps this package to domain and ports.
//
// # What a gate run does
//
//  1. Load the change's log through ports.EventStore and fold it. A log that fails verification gets a state.reset
//     first (see below), and the run continues from the new log.
//  2. Read GitHub for the current head: the pull request, its files, its reviews, and the policy at the merge base.
//     Every read that fails makes its fact unknown, never missing-in-a-good-way.
//  3. Build fastcheck.Facts and run fastcheck.Evaluate. Only testintegrity.Result.Blocking() reaches the clause; a
//     warning never blocks.
//  4. Call actionflow.Decide with HeadAt set to the time the head was observed (the last pr.opened or pr.pushed event),
//     so the sweep row can tell how long a proof result has waited.
//  5. Run the effects the rows ask for: append the events, post the two check runs, and post or update the summary
//     comment. A proof row's effects are left to the proof use case.
//
// # Invariants
//
//   - Fail closed. A fact that cannot be read is unknown, and an unknown clause never reads as met (fastcheck). An
//     unreadable log, or an unreadable pull request, stops the run before any write that could look like a pass.
//   - Idempotent. A second run on the same head appends nothing new, posts no second check run (the external id names
//     the head and the check), and updates the one summary comment in place. The summary is found by its hidden marker
//     and the author login of the token, exactly as ACTION_MODE 2.5 says; the log's marker is a pointer for display and
//     no decision reads it.
//   - The log is the only memory. Decision input comes from the fold of the log plus GitHub, never from a field of this
//     package.
//   - Untrusted input stays untrusted. Comment bodies, paths, analyzer messages and claims are bounded by the domain
//     that reads them (external, fastcheck, sarif) and are escaped by the summary renderer. This package never treats
//     an artifact, a comment or a check run as a fact that can make a check pass.
//   - Pure dependency rule (R3). The package imports the standard library, internal/domain and internal/ports only.
//
// # A log that fails verification
//
// The MAC and the hash chain protect produced facts against a writer (ACTION_MODE 2.7). On either failure the gate
// appends nothing to that log. It asks Config.Resetter for a new log whose first event is state.reset{reason}, then
// reads the observed facts again and makes the produced ones again. Package actionflow gives every other row the state
// unknown until the reset is recorded, so a forged or broken log can never leave a check saying success.
//
// # Failure modes
//
// A log that fails verification without a Resetter returns ErrStateUnverified: nothing is posted and the log is not
// touched. A pull request that cannot be read, a policy that does not decode, and a version conflict that does not
// clear are errors naming what failed; the run then posts nothing, because a check without facts would be a guess.
//
// # Not here
//
// The proof run (AM14b), the CLI wiring (AM15), the workflows (AM16), the summary wording (A3), and the fast-check
// clause rules (A2, A4, A6), which live in the domain.
package action
