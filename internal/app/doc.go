// Package app holds the use cases (HandleWebhook, SubmitReview, EvaluateGate and so on). It imports
// domain and ports only; adapters are wired in cmd.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// The design note (purpose, API, invariants, failure modes, tests) is written here before the first code.
package app
