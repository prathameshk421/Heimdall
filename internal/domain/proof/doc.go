// Package proof is the rule behind the heimdall/proof check of Action mode: it decides whether a change needs a proof
// run, turns the facts of a run into one outcome, and maps that outcome to a verdict and a check conclusion
// (ACTION_MODE 5.2, 5.4, 7).
//
// # Purpose
//
// The proof run is async and model driven, and it may say "I do not know". This package holds the part that is plain
// logic: the trivial-skip rule, the budget arithmetic, the outcome with its reasons, and the 5.4 table. It does no I/O,
// reads no clock and calls no model. The caller runs the loop and the sandbox, and fills Facts. The summary renderer
// (work package A3) reads Outcome and Counts. This package does not render text beyond the one headline line.
//
// # API
//
//   - Trivial(files, action) Decision: skip the proof run or not, with a stable reason (TrivialWhy) and one sentence.
//   - Evaluate(Facts) Outcome: the kind (skipped_trivial, proven, blocked, not_reproduced, unknown), the Reason of an
//     unknown, the Counts of candidates, and the Cost.
//   - Outcome.Verdict() gate.Verdict: pass, fail or unknown. Outcome.Conclusion(policy.Action) Conclusion: the
//     conclusion for the mode. Outcome.Headline(): the first line of the summary.
//   - MonthlyStop(action, monthSpent) Reason and Spend.Exceeds(budget): the money checks. Reason, StopReasons: the
//     reasons, as stable words with a text for people.
//
// # Invariants
//
//   - Fail closed. Only a skip and a proven run map to pass. not_reproduced and unknown map to unknown, blocked maps to
//     fail, and a kind nobody defined maps to fail. In enforce mode with the proof required, unknown is
//     action_required and never success.
//   - An unknown always names its reason. A malformed fact gives "unknown: no result", never a pass.
//   - A known failure is not hidden by a missing fact: a proven blocking finding makes the run blocked even when the run
//     later hit its budget or deadline. The exception is a run whose results cannot be trusted at all (superseded, state
//     failed verification, workflow changed): that is unknown.
//   - A run that hits the cost cap is unknown, not fail and not pass. A spend equal to the cap is a finished run. A
//     spend that cannot be true (negative, infinite, not a number) counts as over the cap.
//   - A trivial skip costs $0. Evaluate reports an empty Cost for it, whatever spend the caller passes.
//   - Heimdall's own files are never trivial: heimdall.yaml, .heimdall/, Heimdall's workflows and the workflow of a
//     declared input. The rule matches the heimdall_files clause of package fastcheck.
//   - Policy globs come from the action area at the merge base, filled by policy.Action.Effective. No other key is read.
//
// # Decisions
//
// The design is silent or open on these points, so the strict choice was made.
//
//   - Not reproduced is unknown, not fail. The design says a test that passes on base proves nothing and is dropped
//     (4.3). With none left, the change is not shown wrong, only not shown right. It blocks in enforce mode with the proof
//     required (action_required), and it is neutral otherwise.
//   - A trivial skip is a pass. The rule is deterministic and strict, and a required check that failed every docs-only
//     change would be turned off by its users. In shadow mode the conclusion is neutral like every other.
//   - An empty diff is not trivial. A list with no file may mean a missing fact, so the run is not skipped.
//   - The size rule counts every changed line, test files included. The design says "outside tests", but action mode has
//     no tests directory and the design names no test glob, so any split would be a guess. Counting all lines skips less.
//   - The size rule cannot vouch for a file with no counted lines (binary, mode change, pure rename), so such a change
//     is not trivial by size.
//   - A rename counts by both paths. A move from code into docs is not trivial, because code left the tree.
//   - A path that is empty, absolute, not clean, or that has a backslash, or a negative line count, is not trivial.
//   - A candidate not checked "by design" (trivial PR, a style claim) does not make the run unknown. A candidate not
//     checked for budget, time, a missing key or isolation does (7: unfinished items become unknown).
//   - NeedsPerson counts proven findings that do not block. The design names "needs a person" without a rule, and this
//     is the one case where a finding is proven and the policy still leaves the decision to a person.
//   - An unrecognised mode is read as enforce with the proof required. Policy validation refuses such a mode, so this
//     only guards a caller that skipped validation, and a typo must not switch the gate off.
//   - The monthly check treats a spend plus one per-PR budget exactly at the cap as allowed, as 7 says "over the cap".
//
// # Failure modes
//
// Evaluate and Trivial never fail. Facts that cannot be true give unknown: no result, or a non-trivial decision. Two proof
// runs that start together can pass the monthly cap by their number times the per-PR budget, because the proof job cannot
// reserve atomically (7). The provider's spend limit on the key is the hard stop.
//
// # Tests
//
// trivial_test.go holds the trivial-skip table: docs only, mixed docs and code, a config file the policy names, Heimdall's
// files, the empty diff, renames, bad paths and the size rule. outcome_test.go holds one case per outcome and per reason,
// the counts, the cost, the $0 assertion for a docs-only change, and the monthly and per-PR cap boundaries.
// conclusion_test.go holds the 5.4 table for every mode and outcome, and a check that success only ever follows a pass.
//
// What this package does not do: run the loop, read the state ref, check the budget before each call (the budget guard
// in app does that), decide which findings block (gate.BlockingSet does that), or render the summary.
package proof
