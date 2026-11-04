// Package transport: see the note below.
//
// Inbound edges: webhook, mcp, web (html/template with htmx) and sse. They call app and never import
// adapters.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// Design note (purpose, API, invariants, failure modes, tests) is added here before the first code (T##).
package transport
