// Package postgres is the PostgreSQL adapter of the event store and the outbox (TECH_SPEC 2.3, 2.4, 4.1, 4.2). It
// implements ports.EventStore and ports.Outbox on one *pgxpool.Pool, owns the four tables of 4.1 (event, outbox,
// webhook_delivery, cursor) and the goose migrations that create them, and verifies the hash chain of every change.
//
// Public API
//   - Migrate(ctx, pool): applies the embedded migrations (forward only). Safe to run on every boot.
//   - NewStore(pool) *Store: implements ports.EventStore (Append, Load) and ports.Outbox (Pending, Claim, Done, Fail).
//   - Store.OpenRooms: discovers persisted nonterminal changes with rooms by validating and replaying event history.
//   - Store.GetCursor, SetCursor: the cursor table (pollers, D64; the Switch stream, D46). A missing cursor is
//     ports.ErrNotFound.
//   - Store.SeenDelivery(provider, id): webhook dedupe. It returns true when the delivery was already recorded.
//   - Store.VerifyAll(ctx) (VerifyReport, error): checks the chain of every change and returns the verified heads and
//     the broken changes. The nightly job publishes the heads; this package does not.
//
// Append (TECH_SPEC 2.4). One transaction: take pg_advisory_xact_lock(hashtext(change_id)); read the stored head
// (version and hash); if the head is not the expected version, return ports.ErrVersionConflict; compute prev_hash and
// hash of each event with change.Chain from the stored head, so the store, not the caller, writes the hash chain;
// insert the events; insert the effects, caused by the last event of the batch; commit. The caller's PrevHash and
// Hash values are replaced, and its occurred_at is cut to microseconds before hashing, because Postgres keeps
// microseconds: the hash must cover what is read back. Load returns events in version order, and change.VerifyChain
// passes on them even though JSONB reorders payload keys (the canonical form sorts them).
//
// Effects. An effect whose key is already committed is a no-op when kind, target and payload are the same, and the
// whole append is refused when they differ, because a deterministic key must always name the same action (TECH_SPEC
// 2.4). The effect key is UNIQUE in the table, so the check cannot be skipped by a racing writer.
//
// Outbox lifecycle. Pending returns effects with done_at NULL and attempts below 8, oldest first, as a plain select
// that takes no lease; it is for inspection. Claim (T22) takes a lease: in one UPDATE it selects the oldest rows whose
// lease is free or expired with FOR UPDATE SKIP LOCKED, and sets claimed_until = now() + lease, so the lease starts on
// the database clock. Two live leases never cover one row, and a row locked by another transaction is skipped rather
// than waited for. Done sets done_at once, is idempotent, and clears the lease. Fail adds one attempt, records the
// reason and clears the lease, so a failed row is claimable at once; after eight attempts the row is in the outbox_dead
// view and out of Pending and Claim. A Fail after Done is refused, because it would contradict a performed effect. The
// triggers allow UPDATE only of done_at, attempts, last_error and claimed_until; DELETE and TRUNCATE are refused on
// outbox as on event.
//
// Invariants.
//   - Triggers on event refuse UPDATE, DELETE and TRUNCATE (4.1). Triggers on outbox refuse DELETE and TRUNCATE and any
//     UPDATE that changes a column other than done_at, attempts, last_error or claimed_until.
//   - UNIQUE (change_id, version) on event: at most one event per version of a change.
//   - A unique violation on that constraint is reported as ports.ErrVersionConflict (translateAppendErr), and the
//     database error stays in the chain.
//   - Every stored hash is 32 bytes; a stored value of another length is refused, never truncated.
//
// Schema additions to TECH_SPEC 4.1, both made here and to be copied back into the spec:
//   - outbox.target TEXT NOT NULL: Effect.Target must round-trip, and the spec's table had no column for it.
//   - the outbox_dead view (done_at NULL and attempts >= 8): the dead-letter view that TECH_SPEC 2.1 names.
//   - outbox.claimed_until TIMESTAMPTZ (migration 0002, T22): the end of the drainer's lease. The guard function is
//     replaced to allow this one column; the rest of the guard is unchanged.
//   - the migrations also create goose's own version table, which is bookkeeping and not part of the spec.
//
// Failure modes. A database that is down makes every call return a wrapped error; nothing is partially written
// because each Append is one transaction. A transaction that fails in the middle (for example a duplicate event id
// on the second event) rolls back completely, including its outbox rows. A stored chain that has been edited fails
// VerifyAll with change.ErrChainBroken. A chain that has lost its tail still verifies as a shorter chain, so the
// nightly check compares the head with the published one (TECH_SPEC 4.2).
//
// Concurrency. Two appends at the same expected version: one commits, the other gets ports.ErrVersionConflict and
// writes nothing. Appends to different changes do not wait for each other (the lock is keyed by a hash of the change
// id, so unrelated changes can rarely share a lock).
//
// Tests (build tag component; a real PostgreSQL 16 in Docker started by the test kit, never skipped). Each top-level
// test starts one container and gives each subtest a fresh migrated database. Covered: the happy path and the chain
// after Load; stale and ahead-of-head expected versions; eight racing appends with exactly one winner; a mid-batch
// failure leaving nothing; an invalid event and a non-consecutive batch refused before any write; an effect without a
// cause refused; duplicate effect keys (same content no-op, other content refused); microsecond truncation and JSONB
// key order; triggers on event and outbox, each checked by its own message; outbox Pending order, limit, done twice,
// eight failures to the dead letter; cursor upsert; webhook dedupe; migrations create the tables and the constraint
// name that translateAppendErr matches; VerifyAll on an empty database, on intact changes, on a payload edited under
// disabled triggers, and on a deleted tail (which must pass, and does). T22 adds Claim (outbox_claim_test.go): oldest
// first with a limit, a live lease hiding a row, the same row content as Pending, eight claimers with no overlap, an
// expired lease handing the row out again, a row locked elsewhere skipped rather than waited for, Done and Fail
// clearing the lease, eight failed claims dead-lettering, and refusal of a non-positive limit or lease. Migration 0002
// applies over rows written under 0001 (migrate_upgrade_test.go). Unit tests (no tag): the error translation.
//
// Kill test: an append whose backend is terminated while it waits for the change lock leaves no rows, and the change
// accepts the next append at version 0. This is the process-level kill the database can observe; a kill between
// two statements of the transaction is rolled back by PostgreSQL in the same way, but no test forces that point.
//
// Mutation checks (component tests, each run on a scratch copy of the package): removing the version check, the
// event update trigger, the event truncate trigger, the outbox guard, the outbox truncate trigger, the ON CONFLICT
// clause, the microsecond cut, the error translation, the chain link, the dead-letter filter and the constraint name
// were each caught. Removing the advisory lock is caught only by the kill test, which asserts that a waiting append
// is actually blocked on the change lock: the concurrency test cannot see the lock, because the UNIQUE (change_id,
// version) constraint gives the racing writer the same outcome without it.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck. This package imports
// the ports and the domain packages change, id and workflow, and the database libraries.
package postgres
