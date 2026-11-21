// Package fastcheck is the rule behind the heimdall/fast check of Action mode: a pure function from the facts of a pull
// request to eight clauses and one verdict (ACTION_MODE 5.1). It uses no model and runs no pull request code.
//
// # Purpose
//
// The server's merge rule (package gate) assumes a ticket, seats and pinned ticket tests. A team that runs Heimdall as a
// GitHub Action has none of them, so this package holds the eight clauses that fit: secrets, analyzers, pinned_tests,
// test_integrity, diff_coverage, approval, memory_rules and heimdall_files. It does no I/O. The caller reads GitHub, the
// git trees and the coverage report, and fills Facts.
//
// # API
//
//   - Evaluate(Facts) Result: every clause with its status and reason, the gate.Decision made from the unmet and unknown
//     ones, and gate.VerdictOf of that decision.
//   - Facts and its groups: SecretFacts, AnalyzerFacts, TestFacts, IntegrityFacts, CoverageFacts, MemoryFacts. Each group
//     has a Known flag.
//   - Status: met, unmet, unknown or not_checked.
//
// # Invariants
//
//   - A group whose Known flag is false makes its clause unknown, never met. Silence never helps a change.
//   - A clause that is known unmet makes the verdict fail even when another clause is unknown (gate.VerdictOf).
//   - not_checked is only for a clause the policy does not ask for: no action.pinned globs, or no
//     action.diff_coverage.min. It adds nothing to the decision.
//   - The policy is the action area at the merge base, filled by policy.Action.Effective. A change cannot edit the policy
//     it is judged by, and a change to Heimdall's own files needs a policy owner on head.
//   - The analyzers clause counts only results that are new (sarif.New). A SARIF level maps to a severity: error is high,
//     warning is medium, note is low.
//   - When the change edits the workflow that produces a declared input (the SARIF artifact or the coverage report), the
//     clause is unknown until a policy owner approves on head. That workflow is also a Heimdall file, so heimdall_files is
//     unmet in the same case.
//   - A policy owner approval counts only when it is approved, on head, from someone other than the author.
//   - Reasons name files, rules and counts. They never carry a secret value.
//   - The approval clause is gate.ApprovalReason, not a copy of it. Only a person's active card with a scope, a rule kind
//     and a check can fail memory_rules (rulecard.Confirmable, CanReachE3). A card with no result is unknown.
//
// # Failure modes
//
// Evaluate never fails. Numbers that cannot be true (covered lines above changed lines, or a negative count) make
// diff_coverage unknown. A change that deletes or edits a pinned test with no owner approval is unmet. When the reviews are
// not known, an exemption cannot be decided, so the clause is unknown.
//
// # Tests
//
// fastcheck_test.go starts from all-green facts and checks four things. The first is that they pass. The second is that
// flipping one fact makes exactly one clause unmet. The third is that removing one fact makes exactly one clause unknown.
// The fourth is the boundaries: the coverage minimum, the analyzer severities, the owner exemption and the not-checked
// clauses.
//
// What this package does not do: map the verdict to a check conclusion (shadow or enforce, ACTION_MODE 5.4), read any
// source, or run the test-integrity rules.
package fastcheck
