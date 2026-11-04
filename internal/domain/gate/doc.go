// Package gate is the merge rule: a pure function from what is known about a change to the decision and the
// plain-language reason for each clause that is not met (TECH_SPEC 12.4 and invariants 2, 3 and 5 in 3.4).
//
// # Purpose
//
// The gate says whether a change may merge. It takes facts and returns a Decision. It does no I/O and reads no
// clock; the caller computes each fact (the tests' hash, the CI results, the findings) and passes it in.
//
// # API
//
//   - Evaluate(Facts) Decision: applies the ten clauses in the order of 12.4. Decision.OK is true exactly when
//     Unmet is empty. Each unmet Clause carries its ClauseID and a reason that names the specifics.
//   - Facts, Testset, ReviewRequest, Approval: the inputs.
//   - BlockingSet([]FindingState, policy.Blocking) []id.FindingID: the findings that hold a merge. E4 never does.
//   - CheckAcceptance: the check name of Heimdall's own acceptance run.
//
// # Invariants
//
//   - Silence never helps. A fact that is not known is unmet. The zero Facts lists all ten clauses.
//   - An approval counts only if it is an approval, its commit equals the head commit, its login is not the pull
//     request author, and its login is an approver for the domains of the changed paths (invariant 2, 3.4).
//   - The policy is compared at the merge base and at head, and a change to it needs a policy owner's approval.
//   - E4 findings are never in the blocking set, whatever the policy or the consensus flag (invariant 3).
//
// # Failure modes
//
// The gate fails closed. A missing check, a check that has not reported on head, a pinned tree hash that does not
// match head, and an unknown policy each make a clause unmet with a reason that says which fact is missing.
//
// # Tests
//
// gate_test.go starts from an all-green Facts and flips one fact per case. Each case asserts that exactly that clause
// is unmet and no other. Further tests cover the zero value, the reasons for a bad approval, the owner exception,
// every single flip refused, and BlockingSet's severity, tier, state and E4 rules. Mutation checks were run on the
// clauses, the approval conditions and BlockingSet; see the report.
package gate
