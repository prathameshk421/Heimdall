// Package change is the event model of one change from ticket to merge (TECH_SPEC 3): the event catalog and its
// envelope, the typed payload of every event, the hash chain that makes the log tamper-evident, the outbox row
// (Effect) that carries every external side effect, and the view of a change folded from its events. Everything here
// is a pure value or a pure function. There is no state machine (AGENT_INTERACTION_ARCH F1).
//
// Public API
//   - EventType, All, Event, Actor, ActorKind: the catalog (3.2) and the envelope every event carries.
//   - Payload, PayloadFor, Decode: the typed content of each event; Decode is the only way consumers read it.
//   - Canonical, Chain, VerifyChain, Hash, ErrChainBroken: the hash chain (4.2).
//   - Effect, EffectKind, EffectKey, IdempotencyKey: the outbox row (2.4, 4.1).
//   - View, Fold, Stage: everything known about a change, folded from its events (view.go).
//   - Missing, Need: what the change still needs, from the workflow table and the policy (missing.go).
//   - CanAppend, CanSubmitTests, CanApproveTests, CanReview, CanReportOnTask, ErrRefused: the checks on a request
//     before it becomes an event (can.go).
//   - EventID, RoomID, SenderID, MessageID, QuestionID, CorrelationID, ActorID, Repo: typed identifiers. Those the id
//     package does not define (events and Switch's ids) are defined here, with the same validate-on-parse rule.
//
// Layout by area. The catalog is split into one file per area: events_core.go, events_tests.go, events_review.go,
// events_checks.go and events_talk.go. Each holds its event type constants, their payload types and its part of the
// payload registry; registry() in payload.go joins the parts. The catalog order (All, in event.go) is the order of
// TECH_SPEC 3.2 and is declared there only.
//
// Invariants (TECH_SPEC 3.4) that this package guards by construction:
//   - Invariant 1: Decode and Event.Validate refuse decision.recorded and approval.recorded from an agent. Fold reads
//     every payload through Decode, so the same refusal reaches it.
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
// Facts and views (view.go, missing.go, can.go). A fact is true whatever we expected, so Fold never refuses one: it
// fails only when a payload does not decode. Each event sets or updates part of the View; nothing changes without an
// event, and slop, CI, the gate and each review keep the head they were about, so a stale value shows as stale. A
// request (an agent's submission, a person's command) is checked by a Can predicate on the current view before it
// becomes an event, and a refusal wraps ErrRefused. Missing is F1 rule 3: a workflow row whose after rows are done and
// which is not done itself is a need, then the clauses of the merge rule (TECH_SPEC 12.4) the view can decide. The fold
// rules, the need words and the JSON shape are the spec in testdata/plan/view/README.md, and the goldens there (the
// recorded KAN-22 and KAN-23 logs) are the tests: view_golden_test.go. Stage is a label for people; nothing branches
// on it.
//
// Tests: the goldens; a table per predicate (can_test.go); payload, envelope and chain tests.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck. This package imports
// the standard library and the domain packages id, finding, workflow and policy only.
package change
