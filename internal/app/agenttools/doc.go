// Package agenttools implements the application-side execution of agent tools (TECH_SPEC 6.2, 6.5, 7, 14.1).
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck. This package imports
// standard library, domain and ports; it never imports transport or another internal/app package.
//
// # Purpose
//
// Provide the application logic that backs the MCP server tools for Heimdall agents:
//   - Current: resolve an agent's active task from TaskIndex and rebuild the assignment from the change stream,
//     including ticket acceptance-criterion refs captured in change.triggered.
//   - Ack: acknowledge an assigned task within the 60-second window.
//   - SubmitTests: record test submissions from test authors with blob-stored contents, enforcing deadline timeouts and idempotency.
//   - SubmitReview: validate review findings (path and line ranges), filing valid findings and the review submission.
//   - SubmitVerdict: record resolver verdicts for disputed cases.
//   - AskHumans: post human-facing questions via an injected Asker and record question.asked events.
//   - Progress: durably append bounded progress reports and enforce the two-minute interval from the event stream.
//
// # Concurrency and Consistency
//
// All mutations are appended to ports.EventStore with optimistic concurrency retry (up to 5 attempts on version conflict).
// Every state transition is validated against change.Reduce before appending.
package agenttools
