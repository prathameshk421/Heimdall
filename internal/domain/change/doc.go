// Package change is the event model of one change from ticket to merge (TECH_SPEC 3): the event catalog and its
// envelope, the typed payload of every event, the hash chain that makes the log tamper-evident, the outbox row
// (Effect) that carries every external side effect, and the state names of the reducer. Everything here is a
// pure value. The reducer that moves a change from state to state is task T21 and is not in this package.
//
// Public API
//   - EventType, All, Event, Actor, ActorKind: the catalog (3.2) and the envelope every event carries.
//   - Payload, PayloadFor, Decode: the typed content of each event; Decode is the only way consumers read it.
//   - Canonical, Chain, VerifyChain, Hash, ErrChainBroken: the hash chain (4.2).
//   - Effect, EffectKind, EffectKey, IdempotencyKey: the outbox row (2.4, 4.1).
//   - State, AllStates: the states of 3.3.
//   - EventID, RoomID, SenderID, MessageID, QuestionID, CorrelationID, ActorID, Repo: typed identifiers. Those the id
//     package does not define (events and Switch's ids) are defined here, with the same validate-on-parse rule.
//
// Invariants (TECH_SPEC 3.4) that this package guards by construction:
//   - Invariant 1: Decode and Event.Validate refuse decision.recorded and approval.recorded from an agent.
//   - Every payload validates every field and reports all problems at once (errors.Join); payloads decode strictly
//     (unknown fields, duplicate keys, trailing data and fractional numbers are refused).
//
// The hash chain (hash.go). The canonical form is fixed: keys sorted, no whitespace, no HTML escaping, UTC
// timestamps, the payload re-encoded from its decoded form, and the hash field left out. It is a documented subset
// of RFC 8785 (see Canonical). Chain is sha256(prev || canonical). VerifyChain checks one change's events: versions
// 1..n with no gaps, the same change id, the genesis event with no previous hash, and each link. A truncated tail is
// a valid shorter chain, so the published head hash is the check for it. The chain is per change, because the
// event store's expected version is per change (TECH_SPEC 2.4); TECH_SPEC 4.2 says "chained globally by seq", and
// that reading is recorded as an open choice in the T11 report.
//
// Failure modes: a payload that is not canonical is refused by Canonical (and so by VerifyChain, which reports it as
// ErrChainBroken at that version). An unknown type is refused by Decode. An idempotency key with a colon in its
// change id or a blank discriminator is refused, because the key must be unambiguous.
//
// Tests: table tests for every identifier, the catalog order and spelling, every payload's refusal cases, golden
// JSON for all 39 payloads (testdata/payloads, generated once and read here), a known-answer hash vector re-derived
// with crypto/sha256, tamper tests for every field of the chain, and fuzz targets for event decoding, payload
// decoding and chain verification. Mutation checks: 14 deliberate faults (canonical form, version off-by-one, genesis,
// duplicate keys, invariant 1, validation bounds, enum membership, strict decoding, chain links) were each caught.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck. This package imports
// the standard library and the domain packages id, finding and workflow only.
package change
