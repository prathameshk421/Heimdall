// Package testselect picks the packages whose tests a change can affect, so the proof run stays inside its time
// budget on a large repository. The full suite runs nightly.
//
// # Purpose
//
// The proof run has 15 minutes (ACTION_MODE section 5.2). On a monorepo "run the tests" can take 45 minutes
// (CRITIC-deepseek section 3 point 8). This package turns the changed files and the package dependency graph
// into the packages to test. It does no I/O: the graph comes from an adapter (internal/adapters/gograph for Go)
// and the caller runs the command.
//
// # API
//
//   - NewGraph(packages) builds a Graph from Package values: import path, directory, imports, has-tests.
//     The zero Graph is the unknown graph.
//   - Select(graph, changedFiles, Policy) returns a Selection: Full and FullReason, or the Packages to test,
//     each with a Reason. Reason.String() is a line for the summary.
//   - Policy has four glob lists in the policy file's dialect (policy.MatchGlob): AlwaysRun (package directories),
//     FullTriggers, Pinned (Heimdall-pinned tests) and Ignore (files no test can read, such as documentation).
//
// # Invariants
//
//   - Fail safe. The result is Full, with a reason, when the graph is unknown, when a changed file matches a
//     trigger, or when it belongs to no package. The built-in triggers are go.mod, go.sum, go.work, go.work.sum
//     and vendor/. A trigger wins over Ignore.
//   - A changed non-test file selects its package and every package that imports it, directly or not, through
//     the reverse import closure. A package without tests is walked through but not selected. Test imports are
//     imports, so a change to a test helper package selects the packages whose tests use it.
//   - A changed _test.go file selects only its own package, even when the graph says the package has no tests.
//     No other package can import a test file.
//   - A Go file outside testdata must sit in a package directory. Any other file belongs to the nearest package
//     directory above it, and selects like a non-test file.
//   - The result is deterministic: packages are sorted by import path and the reason names the first changed
//     file in path order and the shortest import chain.
//
// # Failure modes
//
//   - Select returns an error only for a malformed glob in the policy. A glob that never matches would silently
//     drop a trigger.
//   - Build the graph from the tree the tests run on (the head). A deleted package directory is not in that graph,
//     so its files are unmapped and the result is Full.
//   - A non-Go file inside a nested module is mapped to a package of the outer module. go.mod is a trigger, so a
//     change that adds or edits the nested module runs everything, but a plain edit inside it does not.
//   - Dependencies the graph cannot see (a test that reads a file in another package, a code generator, a
//     network service) are not followed. Add such files to FullTriggers. The nightly full run catches the rest.
//   - Only Go is covered. Other languages need their own adapter that fills the same Graph.
//
// # Tests
//
//   - select_test.go: a table on a synthetic graph (leaf, shared library through a package without tests, test
//     imports, test-only change, cycle, always-run, ignore), a table of every Full reason, the reason strings,
//     a bad glob and the graph refusals.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
package testselect
