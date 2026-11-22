// Package gograph reads the output of `go list -deps -test -json ./...` into the testselect package graph.
//
// # Purpose
//
// Test selection (A13) needs to know which package imports which. The Go tool already knows. The caller runs
//
//	go list -deps -test -json ./...
//
// in the repository root with the head tree checked out, and passes the output to Parse. This package runs no
// command and touches no file.
//
// # API
//
//   - Parse(r, Options{Root, MaxBytes}) returns a testselect.Graph.
//   - ErrMalformed and ErrTooLarge, checked with errors.Is.
//
// # Invariants
//
//   - Only packages of the main module are in the graph. The standard library, dependencies, test variants
//     ("p [p.test]") and the generated test mains ("p.test") are skipped.
//   - A package's imports are the main-module packages named in Imports, TestImports and XTestImports, without
//     itself. An external test package imports its own package, and that is not a dependency.
//   - Directories are relative to Options.Root and slash-separated. A package outside the root is refused.
//   - The input is read as a stream, one record at a time. It is bounded by MaxBytes (default 256 MiB; this
//     repository gives 4 MB) and by 200,000 packages.
//
// # Failure modes
//
//   - Input that is not JSON, is cut off, has no main-module package, lists a package with a load error, lists
//     a package twice or puts a package outside the root returns ErrMalformed. The caller treats that as an unknown
//     graph and runs everything.
//   - Input over the limits returns ErrTooLarge.
//   - Build-constrained files that `go list` leaves out (another GOOS) are not in the graph. The directory of the
//     package still maps them to it.
//
// # Tests
//
//   - gograph_test.go: real output captured once from this repository (testdata/golist.json: main-module
//     records plus two standard library records and one dependency, home paths rewritten), the selections it
//     gives end to end, the refusal table, the size limit, and the self-import rule.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
package gograph
