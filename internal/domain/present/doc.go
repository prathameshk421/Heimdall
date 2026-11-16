// Package present decides what a review shows: the scope of each finding and the budget of line comments (REVIEW_ARCH
// section 7).
//
// # Purpose
//
// A true remark about code the change did not touch is still noise on the change. Scope says where a finding is,
// computed from the diff and the symbol graph, never declared by a model. The budget keeps a review to a few line
// comments, blocking ones first.
//
// # API
//
//   - ScopeOf(location, changedRanges, caused): changed when the location overlaps lines the diff added or changed;
//     caused when the caller's graph links it to a changed symbol; otherwise pre_existing.
//   - Arrange(items, cap): the line comments (blocking first, then severity, then tier, at most limit), the rest
//     collapsed, and every pre_existing item noticed rather than commented.
//
// # Invariants
//
//   - A pre_existing finding never becomes a line comment and never blocks.
//   - Arrange keeps the input order for equal items, so the same review always renders the same way.
//   - Arrange returns each item once, in exactly one of the three lists.
//
// # Failure modes
//
//   - Nothing fails. Both functions are total over their inputs.
//
// # Tests
//
//   - present_test.go: the scope table (overlap, caused, neither, other file) and the budget table (order, cap,
//     pre_existing, empty).
//
// This package does no I/O. Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
package present
