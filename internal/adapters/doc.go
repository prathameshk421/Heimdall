// Package adapters: see the note below.
//
// Implementations of ports for real systems (postgres, github, jira, switch, graphiti, docker,
// blobfs). Each lives in a sub-package, translates vendor errors into domain ones, and never imports
// app or transport.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// Design note (purpose, API, invariants, failure modes, tests) is added here before the first code (T##).
package adapters
