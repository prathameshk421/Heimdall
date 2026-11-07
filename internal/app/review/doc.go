// Package review applies the server-side semantic checks for reviewer submissions.
//
// # Purpose
//
// Submit validates a finding against the change head, verifies citations belong to
// the caller and change, and merges equivalent reports. OutputFilter applies the
// same server-side safety boundary to progress text.
//
// # Invariants
//
// A location exists at the head and is inside the file. Evidence references are
// scoped by both change and caller. A finding outside the diff must explain its
// compat, ops, or security relevance. Equivalent overlapping findings retain all
// reporters and the strongest evidence.
//
// # Failure modes
//
// Correctable submission errors are returned together with field paths so the
// caller can retry. Reference-store failures are operational errors. Filtered
// output returns one generic error; the private cause is sent to an audit sink.
//
// # Tests
//
// submit_test.go covers valid and invalid submissions, citation checks, diff
// scope, deduplication at the Jaccard boundary, and secret/canary/size filtering.
package review
