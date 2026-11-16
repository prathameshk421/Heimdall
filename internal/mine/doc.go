// Package mine finds forgotten companions: files that have always changed together (REVIEW_ARCH 4.6).
//
// # Purpose
//
// If file A changed and file B did not, somebody may have forgotten B: the migration without its schema, the other half
// of a pair. Mine counts pair co-changes over the history of the base branch, keeps the directed rules A => B that pass
// the thresholds, and reports each one as a lead. It is a lead, never a block: its precision as a warning is not known.
//
// # API
//
//   - Mine(commits, limits) returns the rules, sorted by antecedent then consequent.
//   - Companions(rules, changed) returns the forgotten-companion results for one change.
//   - WriteRules and WriteSARIF write the two outputs the goldens in testdata/plan/mine fix, byte for byte.
//
// # Invariants
//
//   - A transaction is a commit with at most one parent and at most MaxFiles distinct files. Merges and large commits are
//     not counted at all, neither for support nor for commits(A).
//   - Rules are over exact paths and are directed: A => B and B => A have their own confidence.
//   - A rule needs support >= MinSupport and confidence >= MinConfidence; confidence is support / commits(A) as float64.
//
// # Failure modes
//
//   - Mine never fails. Reading history and writing output are done by cmd/heimdall-mine, which reports them.
//
// # Tests
//
//   - mine_test.go: the golden history and change in testdata/plan/mine, byte for byte, and the 4.6 rules on a small
//     synthetic history (merges and the 30-file skip).
//
// This package does no I/O. Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
package mine
