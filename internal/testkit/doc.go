// Package testkit is TEST ONLY: doubles, the payload recorder and helpers for tests. Production code
// must never import it (D121); internal/archcheck fails the build if it does.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// The design note (purpose, API, invariants, failure modes, tests) is written here before the first code.
package testkit
