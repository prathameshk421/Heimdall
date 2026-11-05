// Package adapters groups the implementations of ports for real systems (postgres, github, jira,
// switch, graphiti, docker, blobfs). Each lives in a sub-package, translates vendor errors into domain
// ones, and never imports app or transport.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// The design note (purpose, API, invariants, failure modes, tests) is written here before the first code.
package adapters
