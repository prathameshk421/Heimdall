// Package intake is the use case that turns a pull request into events: it reads the PR and its files, works out the
// ticket it belongs to, judges it with the slop rule against the policy at the merge base, and appends pr.opened or
// pr.pushed, then slop.evaluated, with the GitHub check effect that shows the result (TECH_SPEC 12.3, 12.4, D146).
//
// # Purpose
//
// The slop rule is the first gate a pull request meets. A PR that fails it is red on its head before any agent is
// woken. This package does the I/O around the pure rule in internal/domain/slop; the rule itself does no I/O.
//
// # API
//
//   - Config and New(Config) (*Intake, error): the ports (EventStore, PRReader, CodeReader, Clock, IDs). New refuses
//     a nil port. No adapter is named here.
//   - Intake.HandlePR(ctx, repo, n) (Result, error): one call per pull request head. It is the unit of test.
//   - Result: the stream the PR belongs to, whether events were appended, the slop verdict (meaningful only when
//     Appended is true), and the version after the append.
//   - A PR that names a ticket with no change yet starts that change: pr.opened is its first event, and the ticket's
//     trigger joins the stream when it arrives (HARNESS_DESIGN B1).
//   - ErrSecondPR: the PR names a ticket whose change is already past attachment (a PR state), so the reducer refuses
//     another pr.opened. Nothing is appended.
//   - ticketKey (ticket.go): the first ticket key in the branch, then in the title. Case-sensitive, whole token, and
//     a valid change id (id.ParseChangeID).
//
// # The stream
//
// A PR with a ticket appends to the ticket's change (PAY-142). A PR with no ticket appends to its own stream,
// pr-<repo>-<number> (D146, id.PRChangeID), which the reducer starts as UNLINKED_PR. The pr.opened payload also
// carries reviewer agent suggestions derived from CODEOWNERS and the person map at the merge base; the pick row
// reads them from the view when its wait ends, with no repository I/O. Intake only records facts (reconcile.Recorder);
// the reconciler starts the work that follows.
//
// # Policy
//
// heimdall.yaml is read with CodeReader.ReadFile at the PR's base sha, never at the head (TECH_SPEC invariant 5).
// A missing file, a file that does not decode (unknown keys are refused), or a policy that fails Policy.Validate
// fails closed: slop.evaluated has pass=false with the failing rule "policy_invalid", and the check run fails.
// A transport error is returned to the caller and nothing is appended, so the poller tries again later.
//
// # Events and effects
//
// Each handling appends in one Append, with the expected version it loaded:
//   - pr.opened when no pr event for this PR number exists in the stream, otherwise pr.pushed when the head moved;
//   - slop.evaluated (actor: system "intake"), with FailingRules set to the rule ids when the rule fails.
//
// The effect is one github_check row. Its key is change.IdempotencyKey(stream, new version, github_check,
// "<repo>:<head sha>"), so a new head or a new version gives a new key. Payload: a ports.Check named heimdall/gate.
//   - Slop passed: status in_progress, no conclusion. The merge rule (T47) is not evaluated here, so the gate stays
//     pending. The summary says so.
//   - Slop failed: status completed, conclusion failure, and the summary names every failing rule and its message.
//
// # Invariants
//
//   - Handling the same head twice appends nothing: the second call finds a pr event for this PR with the same head
//     and returns Appended false.
//   - The events of one handling are appended together or not at all.
//   - A PR is a fact and is never refused (AGENT_INTERACTION_ARCH F1). The one rule: a ticket whose change already
//     has a live pull request does not take a different one (ErrSecondPR), and nothing is appended.
//   - A version conflict reloads the stream and decides again: one attempt plus five retries (TECH_SPEC 2.4). After
//     that the error wraps ports.ErrVersionConflict.
//   - The slop facts come from the head: the head's tests tree decides HeimdallTestsExist, through CodeReader.TreeHash.
//
// # Choices and gaps (see the T46 report)
//
//   - ExpectedPaths come from the change's current test submission (tests.submitted expected_paths). A path outside
//     them and the tests directory needs a non-empty "Justification" section in the PR body.
//   - The policy's tests dir is used for the tests check. When the policy is invalid, no slop facts are built.
//   - Ticket keys are case-sensitive. A lowercase branch such as pay-142 names no ticket, so the PR is unlinked.
//
// # Failure modes
//
// Missing change: the PR starts it. A second PR on a ticket in a PR state: ErrSecondPR, nothing appended.
// A port error: wrapped and returned, nothing appended. A stream whose history does not replay: an error naming the
// event; nothing is appended.
//
// # Tests
//
// Table tests with in-memory fakes of EventStore, PRReader, CodeReader, Clock and IDs. They cover: the bad PR naming
// its rules, the good PR passing, the unlinked stream id, the policy read at the base sha (the fake records each sha),
// invalid and missing policy failing closed, a transport error appending nothing, idempotency, a moved head, the
// conflict retry and its exhaustion, ErrSecondPR, and a PR attached before BUILDING.
package intake
