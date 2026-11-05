// Package security holds hmac checks, untrusted-text wrapping, the output filter, injection heuristics
// and redaction rules.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// The design note (purpose, API, invariants, failure modes, tests) is written here before the first code.
package security
