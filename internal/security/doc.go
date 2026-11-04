// Package security: see the note below.
//
// hmac checks, untrusted-text wrapping, the output filter, injection heuristics and redaction rules.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// Design note (purpose, API, invariants, failure modes, tests) is added here before the first code (T##).
package security
