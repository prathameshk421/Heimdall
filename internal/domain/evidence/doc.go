// Package evidence models evidence tiers E1 to E4 and what each is allowed to do. Only evidence the
// code verified can block a merge.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// The design note (purpose, API, invariants, failure modes, tests) is written here before the first code.
package evidence
