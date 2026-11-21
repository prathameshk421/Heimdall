// Package testintegrity holds the pure rules that decide whether a test change can be trusted as proof (work packages
// A6 and the rule half of A11, ACTION_MODE 5.1 and STRATEGY section 0, item 4). It reads facts the caller extracts and
// returns violations with stable ids. It reads no file, runs no test and calls no model.
//
// # Purpose
//
// An agent can make a check go green without fixing anything: it edits the red test, deletes or skips it, mocks the
// code under test, or writes an assertion that cannot fail (research 02, theme 3). The fast check clause test_integrity
// (package fastcheck) reports what these rules find. The proof run uses Prove to decide whether a finding's test is
// proof. The caller parses the diff and the test files. This package never opens one.
//
// # API
//
//   - Evaluate(Facts, Options) Result: the violations, sorted. Result.Blocking and Result.Warnings split them by level.
//   - Violation: ID (rule, path and subject, stable), Rule, Level, Path, Subject and one Message sentence. Rule is the
//     text of a fastcheck IntegrityViolation. The caller maps Blocking() to IntegrityFacts, because that clause treats
//     every violation it gets as unmet.
//   - Prove(ProofFacts) ProofResult: proven, not_reproduced, flaky or build_error, with a stable ProofWhy and a reason.
//   - Retries([]RunOutcome) Stability: pass, fail, build_error, flaky or no_runs for the runs of one test on one tree.
//   - LanguageOf(path) Language: go, python, js (TypeScript too), java (Kotlin too), rust.
//
// # Rules
//
//  1. Pinned tests are read-only (blocks). Any edit, delete or move of a pinned file, or of a pinned function in a file,
//     is pinned-test-edited or pinned-test-deleted. A policy owner who approved head clears it (OwnerApproved with
//     OwnerKnown). The caller derives both flags as fastcheck does: an approved review, on head, from someone other than
//     the author, by a policy owner at the merge base. A missing fact never clears it: OwnerKnown false keeps the
//     violation and says why.
//  2. Weakened tests (warns, blocks in a file pinned as a whole). In a test file: test-deleted (a deleted function,
//     file or move out of the test paths), assertion-deleted (more assertions removed than added), skip-added
//     (t.Skip, pytest.mark.skip, skipif and xfail, unittest.skip, it.skip, test.skip, describe.skip, xit, xdescribe,
//     @Disabled, @Ignore, #[ignore]), timeout-loosened (a timeout number that grows, with Go time units and
//     ms, s, m and h suffixes) and retry-loosened (a retry or flaky marker added, or its count raised). The same level
//     applies to tautological-assertion: an added assertion on a constant, or one that compares a text with itself.
//     When a policy owner approves, the weakening of a pinned file falls back to a warning.
//  3. A test mocks a file the same change edits (mocks-changed-file, warns by default, Options.MockLevel can raise it).
//     The mock must be added by the change (MockFact.Added). Go: a fake of an interface in a changed file of the same
//     package (MockGoFake), or a gomock or mockery package for a changed package (MockGoGenerated). Python: a patch
//     target whose module is a changed file. JS and TS: jest.mock or vi.mock of a changed path, relative or with an
//     @/, ~/ or src/ alias.
//  4. The proof rule (A11, Prove). A finding's test must fail on the PR head in two separate runs (two different run ids
//     on the head tree) and pass on the tree where the suspected lines are reverted or fixed. One red run is not
//     proof. A build error is not a fail. A run on another tree is stale and does not count.
//  5. A rerun that passes is never a clean pass (Retries). Outcomes that differ on one tree, such as fail then pass, are
//     flaky, and Prove reports flaky, not proven.
//
// # Invariants
//
//   - Pure: same facts, same list, in the same order. No I/O, no clock, no logging.
//   - Only proven shows a finding. not_reproduced, flaky and build_error leave it hidden and counted ("unproven, not
//     shown"). None of them says a finding is false. A test can prove a bug is real, never that a finding is false.
//   - Fail closed. A missing tree, a missing run or an outcome nobody defined never gives proven.
//   - Messages name paths, test names and counts. They never carry file content or a secret.
//
// # False positives and negatives
//
// The line rules read single lines, so they are heuristics, and the cost of a wrong guess is a warning.
//
//   - A marker text inside a string, such as "t.Skip(x)", counts as a skip (false positive). A skip written through a
//     helper or a build tag is not seen (false negative).
//   - A renamed test function looks like a delete plus an add, so it is test-deleted (false positive). Two test classes
//     in one Python file that share a method name look like one test (false negative).
//   - A timeout passed as a variable has no number and is not seen. Units are read from the line itself: Go time
//     constants or an ms, s, m or h suffix. A bare number is taken as it is, so mixing units on one line is wrong.
//   - A same-value assertion that someone wrote on purpose, and two spellings of one value, are the false positive and
//     the false negative of the tautology rule.
//   - Mocks: a Go fake of an interface whose file changed for an unrelated reason, a Python patch of a name whose package
//     __init__.py changed, and a JS alias that ends like a changed file are false positives. A mock built by a helper,
//     an alias the rules do not know, and a fake whose declaring file the caller did not resolve are false negatives.
//
// # Failure modes
//
// Evaluate and Prove never fail. A language the package does not know gets only the file-level rules (a deleted or
// moved test file, pinned tests). A mock fact with an unknown kind is ignored.
//
// # Tests
//
// rules_test.go, pinned_test.go and proof_test.go hold the tables: every rule has a case that flips it and a case that
// must stay quiet, and every outcome of Prove has one. testdata holds small real-looking test files that the rules
// must catch, each with the facts a caller's parser would extract: a Go test that asserts a value equals itself, a Go
// fake and a mockery mock of a changed package, a Python test that patches the function it tests, a TypeScript test
// that mocks the module it tests, and a TypeScript file full of skipped tests.
//
// What this package does not do: parse any language, run a test, choose the tree where the suspected lines are fixed,
// map a violation to a check status (package fastcheck), or word the summary (work package A3).
package testintegrity
