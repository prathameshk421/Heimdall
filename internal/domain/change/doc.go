// Package change is the event model of one change from ticket to merge (TECH_SPEC 3): the event catalog and its
// envelope, the typed payload of every event, the hash chain that makes the log tamper-evident, the outbox row
// (Effect) that carries every external side effect, the state names of the reducer, and the reducer itself. Everything
// here is a pure value or a pure function.
//
// Public API
//   - EventType, All, Event, Actor, ActorKind: the catalog (3.2) and the envelope every event carries.
//   - Payload, PayloadFor, Decode: the typed content of each event; Decode is the only way consumers read it.
//   - Canonical, Chain, VerifyChain, Hash, ErrChainBroken: the hash chain (4.2).
//   - Effect, EffectKind, EffectKey, IdempotencyKey: the outbox row (2.4, 4.1).
//   - State, AllStates: the states of 3.3.
//   - Reduce, Replay, ErrIllegalTransition: the reducer (3.3, task T21).
//   - EventID, RoomID, SenderID, MessageID, QuestionID, CorrelationID, ActorID, Repo: typed identifiers. Those the id
//     package does not define (events and Switch's ids) are defined here, with the same validate-on-parse rule.
//
// Invariants (TECH_SPEC 3.4) that this package guards by construction:
//   - Invariant 1: Decode and Event.Validate refuse decision.recorded and approval.recorded from an agent. Reduce
//     reads decision.recorded through Decode, so the same refusal reaches it.
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
// The reducer (reducer.go). Reduce(state, event) returns the next state, or the state unchanged and an error wrapping
// ErrIllegalTransition. Replay folds Reduce over a stream from the empty state. The rules are one table, built by
// transitions() as state -> event type -> rule. A missing cell is an illegal transition. The table is built on every
// call because package-level mutable state is not allowed in the domain (R7).
//   - Start. The empty state (the zero State, which is not in AllStates) takes change.triggered to TRIGGERED, and a
//     pr.opened with no ticket to UNLINKED_PR, named pr-<repo>-<number> by the caller (D146). A pr.opened that names a
//     ticket cannot start a stream. An empty stream reaches the empty state, which is not an error.
//   - Terminal. MERGED and REJECTED have no cells, so they refuse every event type (TECH_SPEC 3.3).
//   - Everywhere. Every non-terminal state takes change.rejected to REJECTED, and a decision.recorded of kind reject
//     to REJECTED. step.retried and step.escalated leave every non-terminal state where it is (TECH_SPEC 5.1, on_fail); the
//     empty stream and the terminal states refuse them.
//   - Before BUILDING. A pr.opened that names a ticket attaches while the state keeps its place, from TRIGGERED to
//     TESTS_READY (D146). The first pr.pushed or pr.opened in BUILDING moves to PR_OPEN. A pr.closed that did not
//     merge changes nothing before a PR state, and a merged one is refused.
//   - ticket.changed moves WRITING_TESTS through BUILDING back to WRITING_TESTS (3.3).
//   - PR states (PR_OPEN through APPROVED) take pr.pushed to PR_OPEN, and a pr.closed that did not merge to REJECTED.
//   - UNLINKED_PR stays on pr.pushed, ci.reported and a gate that is not green, and pr.linked closes it (see choices).
//
// Events whose payload decides the move, and the field each reads (all through Decode):
//   - pr.opened: ticket present or absent (D146, and which states it may start or attach from).
//   - pr.closed: merged. A merge is only allowed from APPROVED.
//   - slop.evaluated: pass. A PR with no ticket cannot pass.
//   - gate.evaluated: ok. Green in IN_REVIEW moves to READY_FOR_APPROVAL; red from READY_FOR_APPROVAL or APPROVED
//     goes back to IN_REVIEW.
//   - tests.verified: needs_signoff. True sends the tests to AWAITING_TEST_SIGNOFF, false to TESTS_READY.
//   - task.assigned: role. Only the test-author opens WRITING_TESTS.
//   - finding.state_changed: state. fix_claimed starts FIXING, verifying moves it to VERIFYING_FIX, and resolved or
//     open returns the change to IN_REVIEW.
//   - decision.recorded: kind. reject ends the change; uphold, dismiss, spec_change and override resolve ESCALATED.
//
// Every other event moves on its type alone, and its payload is not read by the reducer. Reduce does not check the
// envelope either: callers validate events before they reach it. Replay does not check versions, ids or the hash
// chain. VerifyChain does that.
//
// Choices made for T21, for review. The spec leaves these open, so each one is a decision the owner may change.
//   - The sign-off record (D33, D131) is in the data. tests.verified carries needs_signoff, which the engine sets from
//     policy. With it true, verified tests wait in AWAITING_TEST_SIGNOFF until tests.approved (TESTS_READY); without
//     it, they are TESTS_READY at once. tests.committed is refused while the tests await sign-off, so a sensitive
//     change cannot commit before its approval. A payload that omits needs_signoff decodes as false, so the engine
//     must always send it.
//   - TESTS_READY is left by tests.committed, which is what tells the developer to build (3.3 draws the arrow without
//     naming the event).
//   - pr.linked from UNLINKED_PR moves to REJECTED, the only terminal state that ends a stream without a merge. The
//     new ticket's change is created by the engine with trigger source pr (D146). No new State value was added.
//   - pr.closed that did not merge ends the change (REJECTED) from any PR state and from UNLINKED_PR. Before a PR
//     state it changes nothing, because a PR attached early may close while the ticket still needs one.
//   - A pr.pushed on an unlinked PR keeps it UNLINKED_PR (it stays red), and does not open a PR state.
//   - APPROVED moves on change.merged, or on a pr.closed that merged. A gate that goes red after approval goes back to
//     IN_REVIEW, and so does an approval that is invalidated.
//   - Invariant 2 (an approval counts only at the current head) is for readers. The reducer keeps no head sha, so it
//     cannot check it.
//
// Failure modes: an event with no cell, or refused by its content, returns an error wrapping ErrIllegalTransition
// and leaves the state unchanged. A payload that does not decode returns the decode error, which does not match
// ErrIllegalTransition, so a bad payload and a bad order of events are told apart. An agent's decision is refused by
// Decode (invariant 1) and is not reported as an illegal transition.
//
// Tests: the full state x event-type product, with the expected result of every pair in an independent literal
// (wantNext in reducer_test.go); content-dependent cases, one per decided move; full-life streams from the first
// event to merge, including the sensitive-domain sign-off, the slop loop, deliberation and escalation, the fix loop,
// a PR attached before BUILDING, an unlinked PR, and a ticket change that rewinds; every stream is replayed twice; a
// seeded random walk over legal moves checks that each default move matches the literal and that replay agrees.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck. This package imports
// the standard library and the domain packages id, finding and workflow only.
package change
