// Package slop: see the note below.
//
// The door: pure rules that turn away oversized or ticketless pull requests before any agent is woken.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// Design note (purpose, API, invariants, failure modes, tests) is added here before the first code (T##).
package slop
