// Package ports: see the note below.
//
// The interfaces the application needs: EventStore, Projections, VCS, Tracker, Chat, AgentRuntime,
// Sandbox, Memory, Blobs, Clock, IDs. Defined by the consumer, small, imports domain only.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// Design note (purpose, API, invariants, failure modes, tests) is added here before the first code (T##).
package ports
