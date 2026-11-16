// Package sarif reads what a check tool reports and keeps the results that are new in a change.
//
// # Purpose
//
// Every check in the repository profile writes SARIF 2.1.0 (REVIEW_ARCH 4.1, 4.4). This package turns one run's SARIF
// into Result values, and New keeps the head results whose fingerprint no base result has (REVIEW_ARCH 4.3). It holds
// no tool name: a check's id and lens come from its profile row, which the caller passes in.
//
// # API
//
//   - Parse(data, check, lens) reads one SARIF document into Results.
//   - New(base, head) returns the head results that are new.
//   - Result: the shape of 4.4. Finger is rule:file:hex(sha256(collapsed snippet text)).
//
// # Invariants
//
//   - The fingerprint hashes the text of the reported lines with every run of white space made one space and both
//     ends trimmed. Line numbers are not in it, so moved code is not new.
//   - A result with a region has its text in region.snippet.text. Without it the result cannot be fingerprinted and
//     Parse refuses the document. A result with no region at all (a file-level result) has Lines [0, 0] and the
//     fingerprint rule:file: with no hash; New then matches it by rule and file only.
//   - Input larger than MaxBytes, an unknown level and an invalid line range are refused.
//   - New matches on check and fingerprint together, so two checks that flag the same text do not hide each other.
//
// # Failure modes
//
//   - Malformed JSON, or a result without a rule id, returns an error naming the result index.
//   - New never fails: it is a pure function over values that Parse already checked.
//
// # Tests
//
//   - sarif_test.go: the goldens in testdata/plan/sarif (every pair's New), and FuzzParse, which must never panic.
//
// This package does no I/O. Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
package sarif
