// Package localrunner executes Go tests of a repository in a clean temporary directory with a hard timeout
// and returns JUnit-shaped results (TECH_SPEC 12.1, 12.2, D39).
//
// # Purpose
//
// The local runner implements ports.Sandbox for local and demonstration environments without requiring Docker
// or rootless container daemons. It extracts the untrusted repository tarball with path-traversal safeguards,
// executes go test with a cleaned environment and strict process-group isolation, converts the go test -json
// event stream into a verify.Report and ports.SandboxResult, and cleans up temporary directories reliably.
//
// # API
//
//   - New(src Source, opts ...Option) builds a Runner.
//   - Runner.Run(ctx, job) runs the job and returns ports.SandboxResult.
//   - Runner.RunWithLog(ctx, job) runs the job and returns ports.SandboxResult along with raw log bytes.
//   - ParseEvents(r) converts a go test -json stream into verify.Report, raw log bytes, and error.
//   - ReportToSandboxResult(rep, logRef) converts verify.Report into ports.SandboxResult.
//
// # Invariants
//
//   - Untrusted repository tarballs are bounded to 64 MiB total uncompressed size and 20,000 files.
//   - Tarball extraction rejects path traversal (..), absolute paths, symlinks pointing outside, and device files.
//   - Execution occurs strictly inside the temporary directory.
//   - The parent process environment is never forwarded (no tokens or credentials leak).
//   - The process group is killed at the timeout deadline, leaving no orphaned child processes.
//   - Output buffers are capped at 4 MiB each.
//   - The temporary directory is always removed upon completion.
//
// # Failure modes
//
//   - Invalid job parameters, disallowed commands, or tarball violations fail before process execution.
//   - Process timeouts terminate the process group and report a status error.
//   - Compiler errors produce error cases with compiler output.
//   - Assertion failures produce failure cases with failure traces.
//
// # Tests
//
//   - runner_test.go exercises passing tests, assertion failures, compile errors, panics, timeouts, environment isolation,
//     tarball traversal/escape defenses, size bounds, and process-group tree termination.
//   - junit_test.go tests parsing of go test -json streams, build failures, assertion messages, skips, and unfinished tests.
package localrunner
