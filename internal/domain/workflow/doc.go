// Package workflow holds the vocabulary agents work in: the roles, the kinds of agent work, and the task envelope an
// agent receives. What a change needs, and in what order, is the row table of internal/domain/change (HARNESS_DESIGN B2).
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
//
// # Purpose
//
// Roles and kinds are data in one place, so get_task, the tool table, the wake line, the board and the reconciler
// read one table and nothing switches on a role. The package does no I/O.
//
// # API
//
//   - Role and ParseRole: test-author, reviewer, resolver, profiler.
//   - Kind and Kinds(): one row per kind of agent work: who may hold it, its output schema and the words the wake line
//     and the board use. KindFor(role) finds the kind a role holds.
//   - TaskEnvelope and BlobRef: what get_task returns and how stored content is named.
//
// # Invariants
//
// A role parses only from its fixed names. A BlobRef is blob:sha256: and 64 lowercase hex characters.
//
// # Failure modes
//
// ParseRole and ParseBlobRef refuse anything else, naming the value.
//
// # Tests
//
// kinds_test.go checks the table; task_test.go covers the envelope and its golden.
package workflow
