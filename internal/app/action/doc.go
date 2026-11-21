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
// The proof run is the other half of Action mode (AM14b): it reviews the head tree with a model, proves or drops each
// candidate with a test in the sandbox, and writes a bounded artifact. This package reads that artifact and records it;
// the unprivileged half never sees a write token, and the privileged half never executes PR code.
//
// # API
//
//   - NewIngester(comments) and Ingester.Ingest(ctx, IngestIn): the review comments of one pull request, read through
//     ports.PRReviewCommentReader and mapped by package external. It returns the change.ExternalIngested payloads to
//     record, the candidates that duplicate one of Heimdall's own findings, and the counts of what was dropped or
//     ignored.
//   - NewProof(ProofConfig) and (*Proof).Run(ctx, ProofIn): one proof run. It skips a trivial change at $0, runs the
//     reviewer through ProofConfig.Loop (a ports.AgentLoop, the one loop of the product) with tools bounded by size, and
//     applies testintegrity.Prove to each proposal. The
//     result holds the proof.Outcome, the per-candidate results and the artifact bytes.
//   - NewGate(Config), (*Gate).Run(ctx, changeID): one gate run.
//   - (*Gate).RecordProof(ctx, ProofRecordIn): the gate's read of one proof artifact. It validates the artifact against
//     the head, base, policy hash and workflow run, appends proof.reported and cost.recorded, and records an artifact it
//     cannot trust as unknown: no result.
//   - ParseProofArtifact and ArtifactOutcome: the artifact format, and the gate's read of a file it will not record.
//   - Result: the head, the fast verdict, the events appended, the checks posted, and whether the summary was written.
//   - ErrStateUnverified: the state log failed its MAC or its hash chain. Nothing is written to a log that failed.
//   - ErrProofArtifact: the proof artifact broke a rule of its format. Nothing in it is believed.
//   - Config.Resetter starts a new log with state.reset (ACTION_MODE 2.7). A store that cannot reset leaves it nil, and
//     the gate then fails closed with ErrStateUnverified.
//   - Config.Facts reads the fast-check groups that no port covers: the secret scan, the rule cards at the merge base,
//     and the test-integrity rules. cmd wires it to internal/secretscan and the domain rules, because the dependency
//     rule keeps this package to domain and ports.
//
// # What a proof run does
//
//  1. Decide the trivial skip with proof.Trivial. A skip makes no model call and costs $0.
//  2. Check the monthly cap with proof.MonthlyStop. A month spend that was not read gives no stop.
//  3. Ask the model to review the change. Its tools read only the head checkout, bound every result, and run tests only
//     through the sandbox port. Third-party claims are given to it as claims, never as facts.
//  4. Prove each proposal with testintegrity.Prove: two runs on the head tree and, when both are red, one on a tree with
//     the suspected lines reverted to the base text. A proven finding blocks when gate.BlockingSet says so.
//  5. Write the artifact: version, head, base, policy hash, workflow run, kind, counts, cost and each candidate.
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
//     comment. A proof row that asks for a result the gate makes itself is run here: a trivial change records its skip
//     at $0 (actionflow.SkipProof), and a head whose proof never reported records unknown (actionflow.ReportProofUnknown:
//     the sweep, a changed proof workflow, the monthly cap). Both go through the write RecordProof uses and name
//     RunIn.WorkflowRun, or the run they close. A real proof run's result is left to RecordProof.
//
// # Invariants
//
//   - Fail closed. A fact that cannot be read is unknown, and an unknown clause never reads as met (fastcheck). An
//     unreadable log, or an unreadable pull request, stops the run before any write that could look like a pass. An
//     artifact that is missing, refused or about another run becomes unknown: no result.
//   - The model key never shares a process with PR code. The key stays in this process; every tool that touches PR code
//     goes through the sandbox port or reads the checkout through os.Root. No tool returns the environment, a file
//     outside the checkout, or process information.
//   - No call that could pass a cap. This package hands the per-PR dollar and token caps to the agent loop, and the
//     loop sizes every request before it: the worst case of the request plus the full output cap must fit the caps
//     (ACTION_MODE 7). A run it stops for that reason ends unknown for the reason budget. The guard lives in
//     internal/app/agentloop, so the bench gets it too.
//   - The artifact is untrusted. The gate checks its version, its size, its head, base, policy hash and workflow run, and
//     its counts against its candidates. It also recomputes each proven candidate's blocking flag from the policy at the
//     merge base and refuses a disagreement, because that flag drives the check conclusion.
//   - Idempotent. A second run on the same head appends nothing new, posts no second check run (the external id names
//     the head and the check), and updates the one summary comment in place. A second record of the same proof run
//     appends nothing. The summary is found by its hidden marker and the author login of the token, as ACTION_MODE 2.5
//     says; the log's marker is a pointer for display and no decision reads it.
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
// A proof run that stops for a missing fact (the sandbox, the base version of a line, the model) still writes its
// artifact, with the reason that stopped it. A run whose cost cannot be recorded fails: it reports the error, and the
// sweep closes the missing result as unknown, because a cost that cannot be true must never look free.
//
// # Not here
//
// The CLI wiring (AM15), the workflows (AM16), the summary wording (A3), and the fast-check clause rules (A2, A4, A6),
// which live in the domain. The agent loop is not here: it is internal/app/agentloop behind ports.AgentLoop, so the
// dependency rule (R3) holds and the bench measures the loop that ships. cmd wires an agentloop.Runner, with the
// model and its price, into ProofConfig.Loop.
package action
