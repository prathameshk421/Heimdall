// Package verify holds the pure rules that judge a sandbox run: the JUnit summary, the red-first rule, the flake
// check and the seam import check (TECH_SPEC 12.1, 12.2, D39). It runs no container and does no I/O; the runner
// produces the XML and the test files, and this package decides what they mean.
//
// # Purpose
//
// The test-author's work is only worth what the runner can prove. A test that never fails without the feature
// proves nothing, a compile error is not a red test, a test that changes its mind between runs cannot gate a pull
// request, and a test that reaches into an internal package tests the wrong layer. Each rule here is a function of
// values so the same verdict is reproducible and can be tested without Docker.
//
// # API
//
//   - Parse(io.Reader) and ParseBytes([]byte) read one JUnit XML report into a Report of Suite and Case, with
//     StatusPass, StatusFail, StatusError or StatusSkipped, plus the failure message and body.
//   - Judge(before, after Report, expected map[string][]string) Verdict applies the red-first rule and lists the
//     acceptance criteria no accepted test covers.
//   - Stability(runs []Report) (FlakeReport, error) compares exactly three runs per test and returns Rejected, the
//     flaky tests quarantined from the gate.
//   - SeamImports(files map[string]string, forbiddenPrefixes []string) ([]Import, error) reports a Go test file
//     that imports a forbidden (internal) package directly.
//   - The limits MaxReportBytes and MaxMessageBytes, and the errors ErrTooLarge, ErrMalformed, ErrNoCases,
//     ErrRunCount.
//
// # Invariants
//
//   - A report over 8 MiB is refused before it is parsed, and a failure message or body is cut at 2 KiB on a rune
//     boundary. An endless reader is cut at the limit, not buffered.
//   - A report with zero test cases is refused: a run that ran nothing is never green.
//   - An <error> outranks a <failure>, which outranks <skipped>; the winning element supplies the message.
//   - A test is red-first only when every run of it before the feature is a <failure> that is not a compile, module
//     or panic problem, and every run of it with the feature passes. A test present in one run and missing from the
//     other is rejected and named.
//   - The flake check needs exactly three runs. A test whose status differs between runs, or which is missing from
//     one, is flaky and rejected. A stable failure is not flaky.
//   - Seam prefixes match the package and its subpackages, not a name that merely starts with the same letters. An
//     empty prefix matches nothing. Only *_test.go files are checked.
//   - Verdict, FlakeReport and the violation list are sorted, so the same input gives the same output.
//
// # Failure modes
//
//   - The JUnit reader is hand-written and strict: it refuses a document type declaration, an unknown entity, a
//     nested testsuite, duplicate attributes, stray text, a testcase with no name and any unclosed tag, each with
//     the line number. It expands only the five predefined entities and numeric references, so untrusted sandbox
//     output cannot pull in a document or blow up on an entity.
//   - The import scanner reads import declarations only, skipping comments, string literals and rune literals, so a
//     line that merely mentions an import is not one. A file it cannot scan is an error, never a silent pass.
//   - Encoding/xml and go/parser are not used because the domain lint list (`.golangci.yml`, a guarded file) does
//     not allow them; this is recorded in docs/PROGRESS.md for T29 rather than worked around in that file.
//   - A refused report, a wrong run count and an unscannable file are errors. A test the red-first rule turns away
//     is not an error: it is a rejection with the reason a person can act on.
//
// # Tests
//
//   - junit_test.go reads the ten samples in testdata/junit (assertion failure, compile error, all pass, skipped,
//     two suites, pytest, root testsuite, huge message, empty suite, malformed), with every expected value read by
//     hand off the sample, plus the refusal table, the inclusive size limit, CDATA and entity decoding, status
//     precedence, and FuzzParse.
//   - redfirst_test.go: the accepted red, the passing-before rejection, compile and setup failures, skipped,
//     missing from either run and still red with the feature, uncovered acceptance criteria and sorted output.
//   - flake_test.go: stable pass, stable fail, flaky, mixed failure modes, a test missing from one run, the run
//     count, and suites with the same test name.
//   - seam_test.go: a direct internal import, subpackages and lookalikes, non-test files, comments and literals,
//     cgo, file ordering and the scan-error table.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
package verify
