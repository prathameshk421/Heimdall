// Package evidence: see the note below.
//
// Evidence tiers E1 to E4 and what each is allowed to do. Only code-verified evidence can block.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// Design note (purpose, API, invariants, failure modes, tests) is added here before the first code (T##).
package evidence
