// Package actionflow is the row table of Action mode: given the view of a change and the action policy, it decides
// for each step of a gate run what must happen next and what is missing (ACTION_MODE 8, work package AM13). It is
// the Action-mode twin of the server rows in package change (HARNESS_DESIGN B2): the gate reads the table, runs the
// effects it names, appends the events they produce, and reads the view again.
//
// # Purpose
//
// A gate run is stateless. It reads GitHub and the log, and it must decide what to do from those alone. This package
// is that decision, as one pure function: Decide(In) returns a Row for every id in RowIDs, in table order. Each Row has
// a State (done, needed, waiting, blocked, unknown), a Reason in plain words, and the Effects to request. Missing
// reads the same rows to say what the change waits for, so what the gate does and what it tells people cannot drift
// apart.
//
// # The rows
//
//   - state: done, or needed with ResetState when In.StateProblem says the log failed its MAC or chain check. While
//     it fails, every other row is unknown (proof.ReasonStateUnverified) and asks for nothing. After state.reset the
//     fold has dropped every produced fact, so fast, proof and the findings wait to be made again.
//   - fast: needed with RunFast until a fast result exists for the current head.
//   - ingest: needed with IngestExternal for each candidate the log lacks or holds with other content.
//   - proof: a trivial change needs SkipProof at $0. Otherwise GitHub starts the proof workflow, so the row waits,
//     for the run, for its result, or for a run on the current base and policy. It records unknown itself for a
//     proof workflow the PR edited and for a monthly cap reached before any run.
//   - findings: waiting while a proof run owes results, blocked on a proven blocking finding, unknown for findings
//     the run never reached.
//   - budget: the per-PR spend of the run on the head, or the monthly cap before a run. Over the cap is blocked.
//   - break_glass: a bypass of the fast check is waiting until another approver reviews it.
//   - sweep: a proof run that has not reported SweepAfter after its head becomes unknown: no result.
//   - publish: the summary comment and both checks, when the summary is stale for the head or its version.
//
// # Invariants
//
//   - Pure (R5). No I/O, no clock, no randomness. Time comes in as In.Now and In.HeadAt. The same In gives the same rows.
//   - Fail closed. A fact that is missing, malformed or for another head is never read as a pass. An unreadable policy,
//     a missing PR and an ended change give every row one state and no effect. A mode nobody defined is read as
//     enforce, as package proof reads it.
//   - A new head invalidates the proof and fast results of the old head: a result counts only for the head, base and
//     policy hash it ran on. A result for another head is shown as stale and is never used for a verdict.
//   - A known failure is not hidden by a missing fact: a proven blocking finding is blocked even when the run hit its
//     budget. An unproven finding never blocks (E4 never blocks).
//   - Unknown never becomes success. Only publish posts checks, and it takes every conclusion from package proof and
//     from the fast table of ACTION_MODE 5.4. Shadow mode posts neutral. A break-glass by a login the policy at the
//     merge base does not name, or for an old head, changes nothing.
//   - Effects are values and are safe to run twice. Rows never write events: the gate does that.
//
// # Decisions the design left open
//
//   - The per-PR budget is the spend of the proof run on the current head. A push starts a new run with a new budget, and
//     the monthly cap bounds the sum. The design does not say whether the cap covers all pushes of one PR.
//   - The summary version is a number the use case gives (In.SummaryVersion). The marker's version counts writes, and
//     the rows cannot derive "writes needed" from the view without the log's own count.
//   - A candidate that package external drops (outdated, no location) is not an input here. The log keeps only counts
//     for them, so the ingest row could not tell a dropped comment from a new one.
//   - A finding that arrives after a proof run is unknown, not waiting: the proof workflow runs on a push, not on a
//     comment, so no run is coming for it.
//   - The gate cannot start the proof workflow, so a proof row with no run waits. The sweep is what ends that wait.
//
// # Failure modes
//
// Decide never fails. An In that cannot be true (no policy hash, a negative version, a bad spend) gives a state that is
// not done and an effect that repairs the log, never a pass.
//
// # Tests
//
// rows_test.go holds a table test for each scenario of the work package: a fresh PR, a trivial docs-only PR, a push
// mid-proof, a budget hit and the monthly cap, third-party comments, a proven blocking finding in shadow and enforce
// mode, break-glass recorded and reviewed, and a state reset and a state that fails verification.
package actionflow
