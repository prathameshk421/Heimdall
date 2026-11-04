// Package platform: see the note below.
//
// Configuration, slog setup, OpenTelemetry, River and goose migrations. The only place that reads
// configuration from the environment.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// Design note (purpose, API, invariants, failure modes, tests) is added here before the first code (T##).
package platform
