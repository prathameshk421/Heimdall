// Package coverage reads line coverage reports and measures how much of a changed diff they cover.
//
// # Purpose
//
// The fast check has a diff-coverage clause (ACTION_MODE section 5.1). The customer's test workflow
// uploads a coverage artifact in one of three formats: an LCOV tracefile, a Cobertura XML report, or a
// Go cover profile. This package turns that artifact into one shape, then counts the changed lines that
// the report says ran. It is pure: no I/O, no clock, no logging.
//
// # API
//
//   - ParseLCOV, ParseCobertura and ParseGoCover read one report into a Result.
//   - Diff(res, changed) returns the covered, uncovered and not-instrumented counts per changed file and
//     in total, plus the percentage. changed maps a file path to its changed line numbers.
//   - Result holds Files: a map from a normalized path to its instrumented Lines (line number to hit count).
//   - Options carries the caller's byte limit and an optional path prefix.
//   - Counts.Percent is the percentage of instrumented changed lines that ran.
//
// # Invariants
//
//   - A line in Lines is instrumented. A zero hit count means the line is in the report and did not run.
//     A line absent from the report is not instrumented, and Diff never counts it against coverage.
//   - A changed line is covered when its hit count is above zero. It is uncovered when the report names
//     it with zero hits.
//   - Percent is covered over covered plus uncovered, from 0 to 100. With no instrumented changed line it
//     is 100, because nothing in the diff could be measured.
//   - Every path is forward-slash and relative: a leading slash, a leading ./, a drive letter and a
//     repeated separator are removed, and a path that leaves the root is refused. Options.Prefix is
//     removed after that, so an absolute runner path or a Go module path becomes the path a diff has.
//     Diff normalizes changed paths the same way, so ./a.go and a.go are one file.
//   - A path or line that appears twice in one report is merged: hit counts add. That is how a merged
//     LCOV report and a Go block that spans one line twice behave.
//   - MaxLine bounds one line number and MaxLines bounds the instrumented lines of one report, so a small
//     untrusted input cannot expand into a huge result. maxHits bounds one hit count so merged counts stay
//     inside an int.
//
// # Failure modes
//
//   - Malformed input returns ErrMalformed wrapped with the source line, for example
//     "line 12: DA has 1 fields, want line,hits[,checksum]: malformed coverage report".
//   - Input over Options.MaxBytes, or one that would expand past MaxLines, returns ErrTooLarge. A
//     non-positive MaxBytes and a prefix that leaves the root are caller mistakes and return a plain error.
//   - An unknown LCOV record is refused rather than skipped, because a misspelled DA would otherwise
//     remove a line from the report and make the clause pass more easily. Informational LCOV records are
//     accepted and ignored.
//   - The Cobertura reader is hand-written because the domain import list does not allow encoding/xml. It
//     expands the five XML entities and numeric references only, refuses an internal document type subset
//     and fetches no external entity, so an untrusted artifact has nothing to expand.
//   - A complete but empty report is valid: it has no instrumented lines. Only a missing mode line, a
//     missing coverage element or an unknown root is refused.
//
// # Tests
//
//   - lcov_test.go, cobertura_test.go and gocover_test.go: a golden report per format in testdata/, the
//     refusal table with its expected line number, the prefix and Windows path cases, the inclusive size
//     limit, and FuzzParseLCOV, FuzzParseCobertura and FuzzParseGoCover, which must never panic.
//   - coverage_test.go: the diff arithmetic table, the duplicate-line and path-normalization cases, the
//     empty diff, and the percentage boundaries.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
package coverage
