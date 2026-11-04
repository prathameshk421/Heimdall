// Package change: see the note below.
//
// One change from ticket to merge: its identity, state and the reducer that folds events into state.
// Pure values; every state and event pair has a table test.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// Design note (purpose, API, invariants, failure modes, tests) is added here before the first code (T##).
package change
