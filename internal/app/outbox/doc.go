// Package outbox drains the transactional outbox: it claims committed effects from ports.Outbox, performs each one
// through the executor of its kind, and records the result (TECH_SPEC 2.4, task T22). It imports the standard
// library, internal/domain/change and internal/ports only.
//
// Public API
//   - Executor: Execute(ctx, effect) error. One executor per effect kind performs the action on the outside system.
//   - Config and New(Config) (*Drainer, error): builds a drainer, or refuses to.
//   - Drainer.RunOnce(ctx) (DrainResult, error): claims one batch and executes it. It is the unit of work and the
//     unit of test.
//   - Drainer.Run(ctx, wake, tick) error: calls RunOnce when wake receives or tick fires, and returns nil when ctx is
//     cancelled. The caller supplies both channels, so this package has no ticker and no sleep; the caller wires
//     time.NewTicker and the event store's notifications.
//   - DrainResult: how many effects the batch claimed, how many were done, how many failed.
//
// The per-effect cycle. RunOnce claims up to Batch effects with a lease of Lease (ports.Outbox.Claim, which takes
// the lease in the database clock). For each effect, in claim order:
//  1. Its kind has an executor: Start(key) records, under the lease, that the executor is about to run. A refused
//     Start ends the batch and the executor does not run. Then the executor runs with a deadline of EffectTimeout and
//     receives the effect, whose Key is the idempotency key the executor must pass to the outside system.
//  2. The executor returned nil: Done(key) releases the lease and marks the row done.
//  3. The executor returned an error, or the kind has no executor: Fail(key, reason) records one attempt, clears the
//     start mark and releases the lease, so the effect is claimable again at once. The reason is err.Error() and
//     nothing else. An executor must therefore keep secrets out of its error messages; this package does not scrub
//     them. After eight failed attempts the effect is in the dead-letter view and Claim stops returning it
//     (ports.Outbox).
//  4. The drainer stopped after Start and before Done or Fail (a crash): the row keeps its start mark. When the lease
//     runs out, the row is in doubt and Claim never returns it (see below).
//
// Invariants.
//   - Construction refuses an empty executor map, a nil or unknown-kind executor, a nil outbox or logger, a batch
//     below one, a non-positive EffectTimeout or Lease, and a Lease that does not exceed Batch*EffectTimeout. The last
//     rule keeps every effect of a batch under its lease: a batch runs one effect after another, so the lease of the
//     last effect starts before the others finish, and the lease must still be live when that effect runs.
//   - The executor map is copied at construction, so later changes to the caller's map have no effect.
//   - Done and Fail run with a context that is not cancelled when ctx is, but bounded by EffectTimeout. An effect that
//     was executed is still recorded as done when the drainer is shutting down.
//   - A failed Done or Fail (the database is down) is returned by RunOnce. The effect keeps its lease and is claimed
//     again when the lease expires. Run logs such errors and keeps going.
//   - A drainer stops a batch early when ctx is cancelled. The effects that were not reached keep their leases and
//     are claimed again when those expire.
//   - Run does not claim again at once after a batch that did nothing but fail, because a failing executor would then
//     spin, so the next batch waits for wake or tick. A full batch that completed at least one effect is followed at
//     once by another batch, because more may be waiting.
//
// What is guaranteed. An effect whose executor has started is never executed a second time by this drainer.
//   - The database never hands one live-leased effect to two drainers (Claim, tested against PostgreSQL).
//   - Start succeeds only under a live lease on an effect that has not started. A drainer that lost its lease cannot
//     start the row, so it runs nothing. Start also refuses a second start of the same row.
//   - A crash after Start and before Done or Fail leaves the row in doubt. The action may have been performed, so
//     the drainer does not perform it again. outbox_in_doubt lists these rows for a person to reconcile with the
//     outside system. This is a decision: TECH_SPEC 2.4 says every committed effect eventually happens, and that
//     holds only for effects that did not start.
//   - A crash after Claim and before Start leaves the row free. The executor never ran, so the next claim performs
//     it once.
//   - A reported failure (the executor returned an error) clears the start mark and the effect is retried. An
//     executor must therefore return an error only when the action did not happen. An executor that cannot tell
//     must return nil, or check the outside system by key before it acts.
//
// What is not guaranteed. Nothing repeats an in-doubt effect for the caller. Such an effect stays unperformed until
// someone reconciles it. Keys still matter for executors: an outside system that treats the key as the identity of
// an action (TECH_SPEC 2.4) makes a retry after a reported failure safe.
//
// Failure modes. A claim that fails (the database is down) is logged and retried on the next wake or tick. A failed
// Start ends the batch before the executor runs, and the lease is left to expire. A failed Done leaves a started row
// that is in doubt once its lease runs out: the action happened, so the row is not repeated. An executor that panics
// crashes the process: panics are not caught (CODING_STANDARDS R6), and the lease is left to expire. An executor that
// blocks past its deadline must return on ctx; if it does not, RunOnce waits.
//
// Tests. Unit tests (no build tag) use an in-memory ports.Outbox with flags instead of leases, so they check the
// drainer's decisions: success, start before execution, a refused start running nothing, error to Fail with its
// reason, unknown kind, batch size, cancellation between effects, failed Done, a crash after execution that is never
// repeated after a restart, the effect deadline, construction refusals and the Run loop. Component tests (build tag
// component, a real PostgreSQL 16) check the database behaviour that matters here: two concurrent drainers over 50
// effects execute each key exactly once when nothing crashes, and a crash after Start and Execute but before Done
// leaves the effect in outbox_in_doubt and never executes it again after the lease runs out. The adapter tests
// (internal/adapters/postgres/outbox_claim_test.go) check Start's refusals and the in-doubt view. Layout and dependency
// rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
package outbox
