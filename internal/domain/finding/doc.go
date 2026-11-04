// Package finding models reviewer findings: their ids (F1, F2 in chat), their tiers and their
// lifecycle (open, upheld, dismissed, resolved).
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// The design note (purpose, API, invariants, failure modes, tests) is written here before the first code.
package finding
