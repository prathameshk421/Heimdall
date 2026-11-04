// Package slop is the door: pure rules that turn away oversized or ticketless pull requests before any
// agent is woken.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// The design note (purpose, API, invariants, failure modes, tests) is written here before the first code.
package slop
