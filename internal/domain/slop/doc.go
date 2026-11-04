// Package slop is the door: a pure rule that turns away a pull request that is too big, has no ticket, has no
// tests, or touches paths its test author did not expect, before any agent is woken (TECH_SPEC 12.3).
//
// # Purpose
//
// Slop is the cheapest refusal in the system: it runs before any agent, so a rejected pull request costs no tokens
// (TECH_SPEC 6.6). It reads facts and policy and returns a result. It does no I/O, and the caller supplies every fact.
//
// # API
//
//   - Evaluate(Facts, policy.Slop, testsDir) Result: applies the four rules and lists every one that fails.
//   - Facts and File: what the rule reads, filled by the caller from GitHub and the test author's submission.
//   - Result and Failure: Pass is true exactly when there are no failures. Each failure has a stable Rule id and a
//     plain-language message.
//   - Rule constants: ticket_linked, size, tests_present, scope.
//   - JustificationHeading: the heading whose section lets a change touch unexpected paths.
//
// # Invariants
//
//   - Failures are reported in rule order and every failing rule is listed, never only the first.
//   - A ticket key matches only as a whole token: PAY-14 does not match PAY-142. The match ignores case, so a
//     lowercase branch name counts.
//   - Size counts added and deleted lines, excluding files matched by policy slop.ignore and everything under the
//     tests directory. The limit is inclusive: exactly the limit passes. A negative count fails the rule.
//   - Scope excludes only the tests directory. slop.ignore affects size and nothing else, as the spec states.
//   - The justification section is checked only for emptiness after trimming. Its text is never read.
//
// # Choices the spec leaves open
//
// The spec names the justification section but not its heading. This package uses "Justification" at any markdown
// level, so "## Justification" counts. Owners should confirm that heading. When the policy sets require_ticket to
// false, ticket_linked passes without a key; the spec's default keeps it on.
//
// # Failure modes
//
// A zero Facts fails ticket_linked and tests_present, so a missing fact never lets a change through. Malformed
// input (a negative count, an invalid glob in policy) never panics: the fuzz test checks Evaluate for that.
//
// # Tests
//
// slop_test.go has one table per rule with the flip that breaks it and the boundary cases (exactly the limit,
// one over, a sibling of the tests directory, a key that is a prefix of another), the all-failures list, and
// FuzzEvaluate, which checks that Evaluate never panics and that Pass agrees with the failure list.
package slop
