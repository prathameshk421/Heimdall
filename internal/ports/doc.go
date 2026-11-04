// Package ports declares the interfaces the application needs: EventStore, Projections, VCS, Tracker,
// Chat, AgentRuntime, Sandbox, Memory, Blobs, Clock and IDs. They are defined by the consumer, kept
// small, and import domain only.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// The design note (purpose, API, invariants, failure modes, tests) is written here before the first code.
package ports
