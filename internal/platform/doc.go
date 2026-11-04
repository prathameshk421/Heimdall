// Package platform holds configuration, slog setup, OpenTelemetry, River and goose migrations. It is
// the only place that reads configuration from the environment.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// The design note (purpose, API, invariants, failure modes, tests) is written here before the first code.
package platform
