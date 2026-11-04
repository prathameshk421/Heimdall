// Package workflow: see the note below.
//
// Workflow definitions: the steps a change goes through. An invalid workflow fails closed.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// Design note (purpose, API, invariants, failure modes, tests) is added here before the first code (T##).
package workflow
