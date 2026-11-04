// Package gate is the merge rule: a pure function from a change's state to the check result and the
// plain-language reasons. Each clause has a test that flips it.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// The design note (purpose, API, invariants, failure modes, tests) is written here before the first code.
package gate
