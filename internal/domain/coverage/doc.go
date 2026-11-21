// Package coverage reads line coverage reports and measures how much of a changed diff they cover.
//
// # Purpose
//
// The fast check has a diff-coverage clause (ACTION_MODE section 5.1). The customer's test workflow
// uploads a coverage artifact in one of three formats: an LCOV tracefile, a Cobertura XML report, or a
// Go cover profile. This package turns that artifact into one shape, then counts the changed lines that
// the report says ran. It is pure: no I/O, no clock, no logging.
//
// This package reads LCOV and Go cover, which are plain text. Cobertura is XML, and decoding an
// untrusted XML artifact is boundary work, so internal/adapters/coverage reads it on encoding/xml and
// builds its Result through the Builder here. The domain allowlist does not include encoding/xml, and
// this package has no XML reader of its own.
//
// # API
//
//   - ParseLCOV and ParseGoCover read one report into a Result.
//   - NewBuilder(opt) returns a Builder. Add(srcLine, rawPath, line, hits) records one instrumented line,
//     CheckFile refuses a bad path when a file record opens, CheckSize applies Options.MaxBytes, and
//     Result returns the lines. The readers here and the Cobertura adapter all build through it, so no
//     format can store a value the others refuse.
//   - Diff(res, changed) returns the covered, uncovered and not-instrumented counts per changed file and
//     in total, the files the report never names, and the percentage. changed maps a file path to its
//     changed line numbers.
//   - DiffReport.Measurable says whether the report describes the whole diff.
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
//     is 100, so Percent alone must never gate anything: read it only when Measurable is true.
//   - Diff fails closed. A changed file the report never names is FileDiff.Missing, counted in
//     DiffReport.MissingFiles, and has no counts. That is different from a file the report names whose
//     changed line is not instrumented (a comment, a blank line): that line is NotInstrumented and does
//     not count against coverage. A wrong Prefix, a monorepo subdirectory or a report from another
//     package makes files Missing, not covered.
//   - Measurable is false when any changed file is Missing, and when Covered plus Uncovered is 0 while at
//     least one changed line was given. It is true for an empty changed map: nothing is missing and
//     there is nothing to measure. The fast check must treat a report that is not measurable as unknown,
//     never as a pass.
//   - Diff measures only the map it is given. Which changed files are source files at all (a docs or yaml
//     change has no coverage line) is the caller's choice: it leaves them out before the call, or they
//     show up as Missing.
//   - Every path is forward-slash and relative: a leading slash, a leading ./, a drive letter and a
//     repeated separator are removed, and a path that leaves the root is refused. Options.Prefix is
//     removed after that, so an absolute runner path or a Go module path becomes the path a diff has.
//     Diff normalizes changed paths the same way, so ./a.go and a.go are one file.
//   - A Go block spans the lines from its start to its end position, and the end position is exclusive. A block
//     that ends at column 1 of a later line holds no character of that line, so that line is left out. This is
//     how a closing brace on its own line stays outside the block, as `go tool cover -html` shows it.
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
//   - A Builder refuses a bad path, line number or hit count with ErrMalformed and the source line the
//     caller gives it. Add normalizes the path itself, so a caller cannot store one that is not.
//   - A complete but empty report is valid: it has no instrumented lines. Only a missing mode line, a
//     missing coverage element or an unknown root is refused.
//
// # Tests
//
//   - lcov_test.go and gocover_test.go: a golden report per format in testdata/, the refusal table with
//     its expected line number, the prefix and Windows path cases, the inclusive size limit, and
//     FuzzParseLCOV and FuzzParseGoCover, which must never panic. The Cobertura golden, refusals and
//     fuzz target are in internal/adapters/coverage.
//   - real_test.go: real reports from google/uuid and spf13/cobra (Go cover, set and atomic), tj/commander
//     (c8), vercel/ms (Jest) and attrs and more-itertools (coverage.py lcov) in testdata/real. Parser totals
//     against the tools' own summaries, diffs built from the projects' git history, a wrong prefix that makes
//     a report not measurable, and the same report with absolute, ./ and Windows paths.
//     docs/product/research/12-coverage-real-reports.md has the commands and results.
//   - coverage_test.go: the diff arithmetic table, the duplicate-line and path-normalization cases, the
//     missing-file and Measurable cases (a file the report never names, a prefix that makes every file
//     missing, a named file with only non-instrumented lines, the empty diff), the percentage
//     boundaries, and the Builder's path, range and limit rules.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
package coverage
