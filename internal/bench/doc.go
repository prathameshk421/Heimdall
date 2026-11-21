// Package bench holds the pure logic of Proof Bench (ROADMAP Stage 2, WP 2.1 and 2.2): an evaluation set built from
// real open-source bugs and clean merged PRs. This step (P2.1a) builds the dataset and proves each case is real. It
// makes no model calls. Later steps replay each case to score Heimdall reviewers and the red-first test agent.
//
// # Purpose
//
// A case is one change from a real repository. A bug case is a change that brought in a bug, plus the commit that
// fixed it. A clean case is a merged PR that no later fix or revert cites. The manifest
// (testdata/bench/cases.yaml) lists the cases. cmd/heimdall-bench fetches each repository and checks each case:
//
//   - bug: the fix must add or change Go tests. Those tests must fail on the fix parent with the fix's test files
//     copied in, and pass on the fix commit (the fail-to-pass rule of SWT-bench). A build error is not a red test.
//   - clean: the case's test command passes at the merge commit.
//
// # Toolchains
//
// A case names its runner with toolchain (Case.Runner): go (the default for a Go case), cargo or vitest. A cargo or
// vitest case also names its workdir (where install and the tests run), its install steps, optional sparse-checkout
// patterns, and, for a bug case, the fix's tests by id. A cargo id is libtest's test name (tests::name). A vitest id is
// the test file from the repository root, a colon, and the describe titles and the test title joined by " > ". Go
// cases keep finding their tests from the fix diff (TestFuncs), so they name none.
//
//   - cargo: the runner adds "-- --exact <names>" to test_command and parses libtest's text output (ParseLibtest). A
//     compile error is a build failure, not a red test.
//   - vitest: the runner adds the test files, --reporter=json and --outputFile, and parses the report
//     (ParseVitestJSON). A test file that does not load (a syntax error, a missing module or export) is a build
//     failure. Vitest does not type-check, so a test that calls a function the fix adds fails at run time with "is not
//     a function": Run.Messages keeps the first line of each failure, so a reader can tell that from the bug.
//   - Rust unit tests live inside the source file. The before tree takes the fix's test module spliced onto its own
//     code (SpliceRustTests), not the fix's whole file. Truth leaves the test module out (SourceTruth).
//
// The red tree is the fix parent, not the intro commit, because later commits can make the fix's tests fail to build
// on an older tree. The command also runs the same tests on the intro commit and its parent and reports the result as
// evidence (IntroEvidence). That evidence does not decide the verdict.
//
// This package is separate from testdata/reviewbench and testdata/testbench, which hold planted bugs in small
// synthetic repos. Proof Bench uses only real history.
//
// # API
//
//   - Parse(yaml) returns the Manifest after it checks every rule below.
//   - ParseDiff(git diff -U0 output) returns the FileDiffs; GoTruth, GoTestFiles and AddedLines read them.
//   - TestFuncs(source, lines) names the top-level Go tests whose bodies hold any of the given lines.
//   - ParseGoTestJSON(go test -json output) returns the Run: top-level test outcomes and build failures.
//   - ParseLibtest(cargo test stdout, stderr) and ParseVitestJSON(report, root) return the Run of the other runners.
//   - SourceTruth, ToolchainTestFiles, RustTestStart and SpliceRustTests read a fix diff for any toolchain.
//   - Decide(targets, before, after) returns the fail-to-pass Verdict for a bug case.
//   - IntroEvidence(intro, introParent) labels the extra intro check.
//   - CommitParents(raw commit object) returns the parent SHAs, so the manifest parents can be checked.
//   - Reconcile(status, verdict) says whether a run contradicts the status that the manifest records.
//   - CostCap (NewCostCap, Reserve and its settle function, Allow, Add, CaseBudget, Share, Covers) is the hard spend
//     limit of a run. One cap is shared by every worker of a parallel run: Reserve holds the most a call can cost until
//     the call settles, so the total never passes the cap. Share gives a starting case what other calls do not hold,
//     and says when only those holds cut it below the per-case budget; it refuses only when the cap cannot cover
//     another case at all. Covers leaves holds out, because each returns to the cap when its call settles. Its cases are in score_test.go, with the concurrent Reserve test.
//   - Summarize(cases) scores a run; TurnSummary(cases) adds up its model-turn and tool-call latencies, so a slow turn
//     is visible in the results file and in docs/sota/BENCH.md.
//
// # Invariants (the manifest rules)
//
//   - Every SHA is 40 lower-case hex characters. A change is never its own parent.
//   - kind is bug or clean. A bug case has a fix; a clean case has none and has no truth.
//   - status is pending, valid or invalid. invalid needs a reason; the other two have none. valid needs truth for a
//     bug case. pending may lack truth, because truth comes from the fix diff (cmd/heimdall-bench truth).
//   - Truth is the old-side line spans of the fix diff in source files that are not tests, test data, type
//     declarations or vendored code (SourceTruth). A pure insertion after line N is the span N to N+1 (clipped to line 1), so a
//     finding next to the gap still counts.
//     Truth lines are on the fix parent tree. They match the intro tree only when no commit between them moved the
//     lines. Mapping them onto the intro diff is the replay step's job (P2.1b).
//   - The validate command checks that the manifest truth equals the truth it computes, so a hand edit cannot drift.
//
// # Failure modes
//
//   - Parse returns one error that lists every broken rule with the case id, so one run shows all of them.
//   - Decide never fails. It returns an invalid Verdict with the reason: no targets, a target missing or not passing
//     after the fix, a build error before, or no target failing before.
//   - A network or timeout failure is not a verdict. The command reports it as an error, never as invalid.
//
// # Tests
//
//   - manifest_test.go: each rule, broken one at a time.
//   - diff_test.go: a real git diff -U0 from stretchr/testify, insertions, deletions, new files and renames off.
//   - gotest_test.go: real go test -json output (pass, fail, subtests, build failure) and test-function extraction.
//   - verdict_test.go: the fail-to-pass decision from recorded outcomes, intro evidence and reconcile.
//   - runners_test.go: real cargo test and Vite+ (vitest) output from pingdotgg/t3code (testdata/bench/samples).
//   - toolchain_test.go and truth_test.go: the toolchain rules, the Rust splice and the truth of each toolchain.
//   - score_test.go: the score of a run, its percentiles, and the cost cap, including eight workers that reserve at
//     once so the shared cap holds.
//
// This package does no I/O. cmd/heimdall-bench runs git, go test, cargo and pnpm.
package bench
