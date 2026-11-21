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
// events_checks.go, events_talk.go and the Action area (events_action*.go, below). Each holds its event type constants, their payload types and its part of the
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
// The Action area (events_action.go, events_action_proof.go, events_action_facts.go, view_action.go). Action mode runs
// Heimdall as a step in the customer's GitHub Actions (docs/product/design/ACTION_MODE.md). Its nine events come after
// the server's 52 in All, so no earlier position moves, and no server log holds one. The design names seven:
// proof.started, proof.reported, cost.recorded, external.ingested, breakglass.recorded, breakglass.reviewed and
// state.reset. The design records a fast check result and a posted summary comment but names no event for them, so
// they follow the pattern of ci.reported and checks.reported: fast.reported and summary.posted. Choices that the
// design leaves open:
//   - Money is an integer. A payload holds no fractions (jsonstrict.go), so cost is whole micro dollars (usd_micros),
//     with the provider's token names (prompt_tokens, completion_tokens). NewCost converts a proof.Spend.
//   - proof.started, proof.reported and cost.recorded carry the GitHub workflow run id (workflow_run). The design lists
//     none for cost.recorded. The id makes a record that is appended twice (a crash after the append, H1) fold to one
//     record, so cost never counts twice.
//   - A budget hit, a timeout or a superseded run is proof.reported with kind unknown and its reason (proof.Reason). A
//     granted break-glass is breakglass.recorded: the gate checks the person against the policy before it records. There
//     is no event for a request that was refused.
//   - proof.reported carries the per-candidate results (proven, dropped with a reason, not checked with a reason) for
//     the findings that have a name in the log, at most MaxProofResults. Fold puts each on its finding (FindingInfo.Proof).
//     The tier and state of a finding still move only by finding events.
//   - fast.reported must carry the verdict its clauses give (gate.VerdictOf), so a forged pass is not a valid event.
//     A clause reason is at most MaxClauseReason bytes; NewFastReported clips a longer one at a rune boundary.
//   - A break-glass and its review cannot come from an agent (invariant 1 applies to them, in payload.go). The fold does
//     not let a person close their own bypass; another approver's review does (ACTION_MODE 5.4).
//   - state.reset drops every produced fact folded before it (fast, proof, summary, candidates, bypasses and the proof
//     result on each finding) and counts itself. Observed facts stay, and cost records stay: the money was spent.
//
// The fold adds one optional part to View (Action) and one to FindingInfo (Proof). Both are absent from the JSON until
// an Action event arrives, so the goldens and every server fold are unchanged. The Action events enter the fold through
// the default branch of applyHuman, and each is folded by a small function of its own.
//
// Tests: the goldens; a table per predicate (can_test.go); payload, envelope and chain tests; for the Action area a
// round trip, a missing-field case and a bad-value case per event (events_action_test.go) and a fold test per event
// (view_action_test.go).
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck. This package imports
// the standard library and the domain packages id, finding, workflow, policy, gate and sarif, and, for the Action
// area, proof, fastcheck and external (their types are what the payloads carry).
package change
