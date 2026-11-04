// Package testkit: see the note below.
//
// TEST ONLY. Doubles, the payload recorder and helpers for tests. Production code must never import it
// (D121); internal/archcheck fails the build if it does.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// Design note (purpose, API, invariants, failure modes, tests) is added here before the first code (T##).
package testkit
