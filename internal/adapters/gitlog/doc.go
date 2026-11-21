// Package gitlog is the event store of Action mode (ACTION_MODE 2, work package AM8). It implements ports.EventStore
// on one git ref, refs/heimdall/state, through GitHub. It needs no database. Every Append is one commit, and the ref
// moves only by a compare-and-swap.
//
// Public API
//   - New(Config) (*Store, error): checks the config. Config.Git reaches GitHub (*github.ActionAPI satisfies it, and the
//     token lives there). The MAC key (at least 32 bytes) comes in through Config. Config.Wait holds the store between
//     swap attempts (clock.Wait in production). This package never reads the environment, never sleeps and never logs.
//   - Store.Append(ctx, change, expected, events, effects): the same contract as the Postgres store.
//   - Store.Load(ctx, change): every event of the change, after the log passed its checks.
//   - ErrIntegrity and *IntegrityError: a log that failed verification. ErrTooLarge and *LimitError: a size limit.
//     ErrEffectKeyReused: an effect key stored with other content.
//
// GitHub facts this package relies on (spike S1, ACTION_MODE_SPIKE.md 1.1 to 1.7, measured Nov 20 and 21 2026)
//   - REST PATCH /git/refs with force false is NOT a compare-and-swap on refs/heimdall/*: a stale write, a sibling, a
//     rewind and an unrelated commit all returned 200. The Git interface therefore leaves UpdateRef out, and the test
//     fake models PATCH as a plain set, so a regression to it fails the tests.
//   - POST /git/refs creates the ref. A second create returns 422 "Reference already exists": the safe
//     create-if-absent of the first append.
//   - GraphQL updateRefs with beforeOid = the tip read, afterOid = the new commit and force false is the
//     compare-and-swap, and it also enforces fast-forward. A stale beforeOid is refused with an untyped "Something
//     went wrong" error. Two writers with the same beforeOid gave exactly one winner in 14 of 14 trials.
//   - GraphQL cannot see refs/heimdall/*, so every read is REST: the ref, the commit, the recursive tree, the blobs.
//   - A token with contents: read gets FORBIDDEN from updateRefs.
//
// Layout of the ref (a subset of ACTION_MODE 2.3)
//
//	changes/<change>.jsonl   one event per line, the same envelope as the Postgres event row
//	changes/<change>.head    {"version", "hash", "effects", "mac"}
//	effects/<change>.jsonl   one outbox row per line: {"event_id", "effect"}
//
// The head holds the version and hash of the last event, the SHA-256 of the effects file, and a MAC. The MAC is
// HMAC-SHA256 over a fixed context string, the change id, the version, the chain head and the effects digest. The
// change id stops a head from moving to another change. The effects digest puts the outbox rows under the same MAC.
//
// Append
//  1. Check the batch with no call: ids, consecutive versions, event validity, 64 KiB per event, and a fresh event id.
//  2. Read the tip, its commit, the recursive tree, and the head and change blobs. Check the MAC first, then the chain
//     (change.VerifyChain), then that the head names the last event. Any failure is an IntegrityError and nothing is
//     written. An append that carries effects also reads and checks the effects file.
//  3. If the change is not at expected, return ports.ErrVersionConflict.
//  4. Chain the new events from the stored head with change.Chain, as the Postgres store does. Occurred_at is cut to
//     microseconds, so a later import into Postgres (AM16) keeps every hash.
//  5. Write the blobs, a tree on the tip's tree, and a commit whose parent is the tip. Then move the ref: CreateRef on
//     the first append, SwapRef (updateRefs) after that.
//  6. A refused move is not typed, so the store reads the tip again over REST and the tip decides. Tip equals the new
//     commit: the move landed and only the answer was lost, so it is a success. Tip equals the old tip: a transient
//     failure, so it waits 1 s, 2 s, 4 s through Config.Wait and offers the same commit again, at most 4 times, then
//     ports.ErrUnavailable. Any other tip: another writer won, so it goes back to step 2. If the change itself moved,
//     step 3 returns ErrVersionConflict. If only other files moved, the commit is rebuilt on the new tip with no new
//     decision, at most 10 times, then ports.ErrUnavailable. On that re-read, a log that already holds this exact
//     batch (by event id) means the move landed and another writer appended on top: a success.
//  7. FORBIDDEN (or 401 or 403) fails closed at once: no re-read, no retry.
//
// Limits (ACTION_MODE 2.6). One event line is at most 64 KiB. One change file or effects file is at most 4 MiB. The
// tree listing is at most 50,000 entries, and a truncated one is refused. A blob states its size, and the size is
// checked before the content is decoded (ports.ErrObjectTooLarge, mapped to a LimitError). Every reply is held to the
// GitHub client's 8 MiB body cap. Object ids from the API must be 40 hex characters.
//
// Security
//   - The MAC is compared with hmac.Equal, which takes constant time. A test reads the source and fails if another
//     comparison is added.
//   - The key and the token are never in an error, a log line or a print. Store and Config print as redacted. The
//     GitHub adapter names the method, the path, the status and a GraphQL error type, never a body or a message.
//   - A bad log is refused. It is never repaired here. The gate starts a new log with state.reset (ACTION_MODE 2.7).
//   - What the MAC cannot see: a writer who puts back an older, valid commit of the whole state (anyone with write
//     access can git push the ref). That rolls a change back to a version that was once true. The reconciler reads
//     the observed facts from GitHub again, so a rollback costs a repeat of work and cannot forge a result that never
//     existed.
//   - In a public repository the ref is world-readable (S1 1.5). The log holds only what the public summary could show.
//
// Failure modes. A missing ref is an empty store. 401, 403 and FORBIDDEN are ports.ErrUnauthorized; 429, a rate-limited
// 403, 5xx and transport errors are ports.ErrUnavailable (the GitHub client's mapping). Only the ref move is retried
// here, and only while the tip shows nothing moved. A crash between the blobs and the move leaves unreferenced objects
// and no state. A cancelled context or a failed re-read after a refused move leaves the outcome unknown: the error says
// so, and the caller's next Load shows the truth.
//
// Cost. Each Load makes 3 to 6 reads. Each Append adds 3 to 5 creates, one move, and on the first swap one read of
// the repository's node id (cached). A refused move adds one ref read.
//
// Not here. ports.Outbox (Pending, Claim, Done, Fail) and the budget file are later work packages. This package
// writes the effect rows so none is lost. The outbox adapter will add "done" rows and must re-sign the head, because
// the head covers the effects file. The git push --force-with-lease route of S1 1.3 is not built (owner decision, R4).
//
// Tests (no network, no tag). fake_test.go models GitHub as S1 measured it on an httptest server, and the store talks
// to it through the real github.ActionAPI: REST reads with a recursive tree, POST refs with 422 on an existing ref,
// GraphQL updateRefs honouring beforeOid with the opaque error on a mismatch, and PATCH as a plain set. Covered: the
// EventStore contract; the request shape (beforeOid = the tip read, force false, no PATCH); a ref moved for another
// change (rebuild), for the same change (ErrVersionConflict), at the first append, and without end (bounded); a lost
// answer (opaque error and 502), with and without another writer on top; a transient refusal retried with backoff, and
// one that never clears; FORBIDDEN; a cancel during backoff; a create refused or lost; two and six writers on one change
// (exactly one wins; run with -count=20, without -race because CGO_ENABLED=0); 20 kinds of tampering, each refused by
// Load and Append with no new commit; a wrong key; size limits of events, files, effects and replies; credentials in
// errors and prints; effect idempotency; parallel appends to other changes.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck. This package imports the
// ports and the domain packages change, id and workflow, and the standard library. Its tests also import
// internal/adapters/github.
package gitlog
