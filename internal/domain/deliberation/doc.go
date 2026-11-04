// Package deliberation models challenges and decisions on findings: who may decide, what counts as new
// evidence, and consensus holds.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// The design note (purpose, API, invariants, failure modes, tests) is written here before the first code.
package deliberation
