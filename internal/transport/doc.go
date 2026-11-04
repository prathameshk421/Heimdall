// Package transport holds the inbound edges: webhook, mcp, web (html/template with htmx) and sse. They
// call app and never import adapters.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// The design note (purpose, API, invariants, failure modes, tests) is written here before the first code.
package transport
