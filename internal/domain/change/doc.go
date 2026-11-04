// Package change models one change from ticket to merge: its identity, its state, and the reducer that
// folds events into state. Everything here is a pure value; every state and event pair has a table
// test.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// The design note (purpose, API, invariants, failure modes, tests) is written here before the first code.
package change
