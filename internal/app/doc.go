// Package app: see the note below.
//
// Use cases (HandleWebhook, SubmitReview, EvaluateGate and so on). Imports domain and ports only;
// adapters are wired in cmd.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// Design note (purpose, API, invariants, failure modes, tests) is added here before the first code (T##).
package app
