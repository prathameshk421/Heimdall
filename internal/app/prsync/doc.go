// Package prsync turns GitHub pull request state (reviews, check runs, closures) into domain change events and runs
// the facts the merge gate reads (TECH_SPEC 2.3, 2.4, 3.4, 12.4).
//
// # Purpose
//
// GitHub reports pull requests, reviews and check runs. This package ingests them into the append-only event stream of
// each change, keeping the stream idempotent, valid and consistent with invariants 1 and 2. It records facts only: the
// reconciler's gate row decides the merge rule from them (HARNESS_DESIGN B5).
//
// # API
//
//   - Config and New(Config) (*Syncer, error): configure repos, ports and use case interfaces. New refuses nil ports
//     and empty repos.
//   - Consumer interfaces: Intake, Reviews, Checks, PRs. Each is small and defined by this package.
//   - Syncer.SyncOnce(ctx) (Result, error): one sync pass across all configured repositories.
//   - Result: counts of synced PRs, appended events, and a slice of FailedPR records.
//   - FailedPR: captures isolated per-PR failures without stopping other pull requests.
//
// # Rules
//
// For each configured repository, OpenPRs lists the pull requests to synchronize. For each pull request:
//
//  1. Intake.HandlePR is called to register the pull request and evaluate slop.
//  2. Closed pull requests: if the PR state is closed, pr.closed is appended once with the merged flag. Reviews and check
//     runs are skipped for closed PRs. An open PR that the stream last recorded as closed without a merge gets one
//     pr.reopened, and the change resumes; a later close appends pr.closed again (found live in H11).
//  3. Reviews: for open pull requests, reviews on the current head commit from logins other than the PR author in state
//     "approved" produce approval.recorded events. Invariant 1 prohibits agents from recording approvals. If an
//     approver later dismisses their review or requests changes, approval.invalidated is appended. Reviews for old heads
//     are ignored. Reviews are deduplicated by review ID.
//  4. Check runs: completed check runs on the PR head commit produce ci.reported events. In-progress or conclusion-less
//     runs append nothing. Check runs for old heads are ignored. Deduplication suppresses runs whose (check, sha,
//     conclusion) matches the latest ci.reported for that check.
//
// # Reconcile (boot and every 5 minutes)
//
// OpenPRs lists open pull requests only, so SyncOnce never sees a PR that closed. Reconcile reads each open pull request
// stream of the event log (Streams.OpenPRStreams) and fetches it with PRs.PR. A PR that GitHub reports closed gets
// pr.closed, once, through the same append as rule 2. A PR that is still open is left to SyncOnce. A failed read goes to
// Result.Failed and the others go on. Reconcile is idempotent: a second call, or a restart, appends nothing for a stream
// that has pr.closed. The server runs it before the first sync and then every reconcileInterval (TECH_SPEC 2.2, D56).
//
// # Consistency and Failure Isolation
//
// Stream updates follow the load-decide-append cycle with optimistic concurrency control. Version conflicts
// (ports.ErrVersionConflict) are retried up to 5 times. Errors on individual pull requests are collected in
// Result.Failed while remaining pull requests continue. A transport outage (ports.ErrUnavailable) on PR listing
// aborts the sync pass and returns the error.
package prsync
