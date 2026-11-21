// Package gitlog is the event store of Action mode (ACTION_MODE 2, work package AM8). It implements ports.EventStore
// on one git ref, refs/heimdall/state, through the GitHub Git Data API (blobs, trees, commits, refs). It needs no
// database. Every Append is one commit, and the ref update is the compare-and-swap.
//
// Public API
//   - New(Config) (*Store, error): checks the config. The MAC key (at least 32 bytes) and the token come in through
//     Config. This package never reads the environment, and it never logs.
//   - Store.Append(ctx, change, expected, events, effects): the same contract as the Postgres store.
//   - Store.Load(ctx, change): every event of the change, after the log passed its checks.
//   - ErrIntegrity and *IntegrityError: a log that failed verification. ErrTooLarge and *LimitError: a size limit.
//     ErrEffectKeyReused: an effect key stored with other content.
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
//  2. Read the tip, the commit, the root tree, the changes tree, and the head and change blobs. Check the MAC first, then
//     the chain (change.VerifyChain), then that the head names the last event. Any failure is an IntegrityError and
//     nothing is written. An append that carries effects also reads and checks the effects file.
//  3. If the change is not at expected, return ports.ErrVersionConflict.
//  4. Chain the new events from the stored head with change.Chain, as the Postgres store does. Occurred_at is cut to
//     microseconds, so a later import into Postgres (AM16) keeps every hash.
//  5. Write the blobs, a tree on the tip's tree, and a commit whose parent is the tip. Then move the ref with force
//     false (or create it on the first append).
//  6. If GitHub refuses the move (409 or 422), read the ref again. A tip that did not change means the refusal has
//     another cause, and the error is returned. A tip that changed means another writer won. Then go back to step 2:
//     if the change itself moved, step 3 returns ErrVersionConflict, and if only other files moved, the commit is
//     rebuilt on the new tip with no new decision. After 10 rebuilds in a row the error wraps ports.ErrUnavailable.
//
// Limits (ACTION_MODE 2.6). One event line is at most 64 KiB. One change file or effects file is at most 4 MiB. Every
// reply of the API is read through a size limit: 64 KiB for refs, commits and created objects, 16 MiB and 50,000
// entries for a tree, and about 4/3 of the file limit for a blob. A blob states its size, and that size is checked
// before the content is decoded. A truncated tree is refused. Object ids from the API must be 40 hex characters.
//
// Security
//   - The MAC is compared with hmac.Equal, which takes constant time. A test reads the source and fails if another
//     comparison is added.
//   - The key and the token are never in an error, a log line or a print. Errors name the method, the path and the
//     status, never a header or a body. Store and Config print as redacted.
//   - A bad log is refused. It is never repaired here. The gate starts a new log with state.reset (ACTION_MODE 2.7).
//   - What the MAC cannot see: a writer who puts back an older, valid commit of the whole state. That rolls a change
//     back to a version that was once true. The reconciler reads the observed facts from GitHub again, so a rollback
//     costs a repeat of work and cannot forge a result that never existed.
//
// Failure modes. A missing ref is an empty store. 401 and 403 map to ports.ErrUnauthorized, 429 and a rate-limited 403
// to ports.ErrUnavailable, 5xx and transport errors to ports.ErrUnavailable. This adapter does not retry them: the
// caller or the outbox does. A crash between the blobs and the ref update leaves unreferenced objects and no state.
//
// Cost. Each Append and each Load makes 5 to 7 reads and, for a write, 3 to 5 creates and one ref call. The changes
// tree is listed whole each time (at most 50,000 entries). That is fine for the volume of ACTION_MODE 2.6.
//
// Not here. ports.Outbox (Pending, Claim, Done, Fail) and the budget file are later work packages. This package
// writes the effect rows so none is lost. The outbox adapter will add "done" rows and must re-sign the head, because
// the head covers the effects file.
//
// Tests (no network, no tag). The fake Git Data API in fake_test.go is an httptest server with blobs, trees with
// base_tree and nested paths, commits, and refs where a ref update with force false is refused unless the commit
// descends from the tip. Covered: the EventStore contract (the Postgres cases that need no database); the shape of the
// requests (force false, parent, base tree, ref create); a ref moved for another change (rebuild), for the same change
// (ErrVersionConflict), at the first append, and without end (bounded); a refusal that is not a moved ref; 18 kinds of
// tampering, each refused by Load and Append with no new commit; a wrong key; size limits of events, files, effects
// and replies; credentials in errors and prints; effect idempotency; parallel appends to other changes.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck. This package imports the
// ports and the domain packages change, id and workflow, and the standard library.
package gitlog
