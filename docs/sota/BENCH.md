# Proof Bench: dataset validation

Work package P2.1a (ROADMAP Stage 2, WP 2.1 and 2.2). This file records the last `heimdall-bench validate` run on the
dataset in `testdata/bench/cases.yaml`. The rules are in `internal/bench/doc.go`. The run makes no model calls and
writes nothing to GitHub.

## What "valid" means

- Bug case: the fix adds or changes top-level Go tests. They fail on the fix parent with the fix's test files copied
  in, and pass on the fix commit. A build error before the fix does not count as a red test. The manifest parents must
  be the real first parents, and the manifest truth must equal the truth computed from the fix diff.
- Clean case: the test command passes at the merge commit, and no GitHub timeline cross-reference cites the PR as a
  fix or revert (checked by hand with `gh api`, 2026-11-21).
- Intro evidence: the same tests also run on the intro commit and its parent. It is evidence only and does not decide
  the verdict.

## Run

| Item | Value |
|---|---|
| Command | `CGO_ENABLED=0 heimdall-bench validate -out run2.json` |
| Heimdall commit | `fa7db3d0abb44cb0a7d37a6089807ffe917e236b` (branch `p2.1-proof-bench`) |
| Started | 2026-11-20T19:07:24Z |
| Machine | darwin/arm64, 8 CPUs (Apple Mac), go1.27.4, git 2.54.0, network on |
| Exit status | 0: every case agrees with its manifest status |
| Wall time | 51.7 s (warm caches) |

Run 1 was the first run, on the same code before the commit, with cold git, module and build caches. It took 311 s
and gave the same outcome for every case. The manifest statuses come from run 1, and run 2 confirmed them. So each
verdict below held on two runs.

## Results

Times are in seconds: run 1 (cold caches) and run 2 (warm caches).

| Case | Lang | Kind | Outcome | Run 1 | Run 2 | Reason or red tests | Intro evidence |
|---|---|---|---|---|---|---|---|
| testify-1207 | go | bug | valid | 14.1 | 3.1 | red: `mock:Test_AssertExpectationsForObjects_Helper` | confirmed: red at the intro, green at its parent |
| testify-1795 | go | bug | valid | 8.0 | 1.7 | red: `mock:TestIssue1227AssertExpectationsForObjectsWithMock` | the fix's tests do not build at the intro |
| gin-4702 | go | bug | valid | 29.6 | 2.9 | red: `TestSaveUploadedFileToExistingDir` | the fix's tests do not build at the intro |
| echo-1799 | go | bug | valid | 28.3 | 3.8 | red: `middleware:TestRewritePath`, `middleware:TestRewriteAfterRouting` | the fix's tests do not build at the intro |
| chi-1185 | go | bug | valid | 11.0 | 2.4 | red: `middleware:TestWrapResponseWriterStatusWhenFlushed` | red at the intro; the tests do not build at its parent, because the intro added the API they test |
| cli-2455 | go | bug | valid | 62.1 | 2.8 | red: `TestCommand_StopOnNthArg` | red at the intro; the tests do not build at its parent, because the intro added the API they test |
| logrus-1564 | go | bug | valid | 16.9 | 1.5 | red: `TestEntryLogPanicLevelDoesNotPanic`, `TestLoggerLogPanicLevelDoesNotPanic` | not run: the 2014 intro has no go.mod |
| cobra-1001 | go | bug | invalid | 0.1 | 0.1 | the fix changes no Go test (the bug is in the bash script) | |
| etcd-19221 | go | bug | invalid | 0.1 | 0.1 | the fix changes no Go test | |
| client-golang-576 | go | bug | invalid | 0.1 | 0.1 | the fix changes no Go test | |
| echo-3004 | go | bug | invalid | 22.2 | 1.1 | the fix's test calls `BindingError.MarshalJSON`, which the fix adds: build error before the fix | |
| chi-1051-clean | go | clean | valid | 32.1 | 26.9 | `go test ./...` passes | |
| echo-2822-clean | go | clean | valid | 19.3 | 2.2 | `go test ./...` passes | |
| cobra-1643-clean | go | clean | valid | 9.0 | 0.8 | `go test ./...` passes | |
| echo-2969-clean | go | clean | invalid | 24.5 | 2.0 | `go test ./...` fails on go1.27.4: `TestHandlerName_differentFuncSameName` | |
| client-golang-2021-clean | go | clean | invalid | 33.5 | 0.3 | `go test ./...` does not build on go1.27.4: undefined `defaultRuntimeMetrics` and others in `go_collector_latest_test.go` | |
| requests-6716 | python | bug | pending | | | no runner for python yet | |
| jackson-databind-5099 | java | bug | pending | | | no runner for java yet | |
| commander-2014 | javascript | bug | pending | | | no runner for javascript yet | |
| zod-6438 | typescript | bug | pending | | | no runner for typescript yet | |
| zod-5959 | typescript | bug | pending | | | no runner for typescript yet | |

## Counts

| Language | Valid bug | Valid clean | Invalid | Pending |
|---|---|---|---|---|
| go | 7 | 3 | 6 (4 bug, 2 clean) | 0 |
| python | 0 | 0 | 0 | 1 bug |
| java | 0 | 0 | 0 | 1 bug |
| javascript | 0 | 0 | 0 | 1 bug |
| typescript | 0 | 0 | 0 | 2 bug |

## Checks that the validator catches a bad manifest

Each run used a copy of the manifest in a scratch directory.

| Tamper | Result |
|---|---|
| testify-1207 truth `510-511` changed to `512-513` | invalid: "the manifest truth differs from the fix diff"; exit 1 |
| chi-1185 fix parent changed to another real commit | invalid: "... is not the first parent of ..."; exit 1 |
| testify-1207 fix SHA changed to a SHA that does not exist | error (not invalid): "remote error: upload-pack: not our ref"; exit 1 |
| `-only nope` | usage error; exit 2 |

## Findings while building the set

- zod#5689 was a clean-case candidate in research/04. Its timeline shows that it caused a regression (issue #5953,
  fixed by #5959). It is now the bug case zod-5959, and it shows why the clean check matters.
- Intro evidence is weak for most cases: the fix's whole test file comes from a later tree, so it often does not
  build at the intro. Only testify-1207 is confirmed. chi-1185 and cli-2455 are red at the intro, and their intro added the API under test. A later step can copy only the target test functions.
- Two clean cases fail on go1.27.4 at their merge commit. This is a toolchain fact on this machine, not a defect of
  the PR. A pinned toolchain per case (from the repo's go.mod) would make clean cases portable.
- Truth lines are on the fix parent tree. For testify-1795 and logrus-1564 the intro is years older than the fix, so
  the lines must be mapped onto the intro diff before a recall score (P2.1b).

## Not verified

- Clean cases: "no later fix" rests on GitHub timeline cross-references only. I did not run SZZ on later fix commits.
  Three clean candidates are under 12 months old (research/04 asks for 12 months).
- caddy: skipped, as the brief asks (189 s suite with 3 failing packages in research/04).
- Python, Java, JavaScript and TypeScript cases: SHAs checked with `gh api`, but no runner exists, so they are not
  validated. They also have no truth yet.
- Flakiness: each verdict held on two runs. I did not run the before and after trees more than once per run.
- The race detector: `go test` runs without `-race` (CGO is off on this Mac), so a race-only bug cannot show red here.

## Test selection

Work package A13. `internal/domain/testselect` picks the packages whose tests a change can affect: the changed
packages and, through the reverse import closure, every package that imports them. A changed `_test.go` file selects
only its own package. A trigger file (go.mod, go.sum, go.work, vendor/, and the policy's list), an unknown graph, or a
file that belongs to no package runs everything. `internal/adapters/gograph` builds the graph from
`go list -deps -test -json ./...`.

### Run

| Item | Value |
|---|---|
| Machine | darwin/arm64, Apple M3, 8 CPUs, 16 GB, go1.27.4, `CGO_ENABLED=0`, network on |
| Heimdall tree | `ed289b6f8da6ce3f69f4c5f32ed77ea34845e1a1` (`product-build`) plus the A13 code (not committed when measured) |
| Commits | The 5 newest first-parent commits of each repo, each diffed against its first parent |
| Repos | this repository (to `ed289b6`), labstack/echo (to `59aacdd`, 2026-11-20), prometheus/client_golang (to `ce65098`, 2026-11-19). Both OSS repos are in `testdata/bench/cases.yaml`; each was cloned to a scratch directory and only read |
| Policy | `full_triggers`: `.github/workflows/**`, `Makefile`, `*.mk`. `ignore`: `*.md`, `LICENSE*`. No `always_run` |
| Timing | One run per cell. `go test -count=1 ./...` against `go test -count=1 <selected packages>`. The build cache was warmed first (`go test -run '^$' ./...`, not timed), so the times are test execution and link time, not compile time |
| Selection cost | `go list -deps -test -json ./...` took 0.2 to 0.6 s. `gograph.Parse` plus `Select` took 11 to 21 ms |

"Packages" counts packages with tests. "Full" says the selection fell back to everything (it never did here).
"Fails" is failing packages in the full run and the selected run.

| Repo | Commit | Changed files | Selected / with tests (all packages) | Full run (s) | Selected run (s) | Fails |
|---|---|---|---|---|---|---|
| heimdall | ed289b6 | 23 | 52 / 94 (111) | 21.6 | 12.3 | 0 / 0 |
| heimdall | 3181e8e | 1 (a `.md` file) | 0 / 94 (111) | 20.4 | 0.0 (no test run) | 0 / 0 |
| heimdall | 060608d | 23 | 1 / 94 (111) | 20.9 | 0.5 | 0 / 0 |
| heimdall | 07d59a5 | 1 (a `.md` file) | 0 / 93 (110) | 21.7 | 0.0 (no test run) | 0 / 0 |
| heimdall | 443c641 | 25 | 1 / 93 (110) | 22.1 | 0.3 | 0 / 0 |
| echo | 59aacdd | 13 | 3 / 3 (3) | 1.7 | 1.7 | 0 / 0 |
| echo | fb1d59d | 7 | 3 / 3 (3) | 1.7 | 1.7 | 0 / 0 |
| echo | 390f278 | 3 | 3 / 3 (3) | 1.7 | 1.7 | 0 / 0 |
| echo | fbd734f | 2 | 3 / 3 (3) | 1.7 | 1.6 | 0 / 0 |
| echo | f275a6b | 4 | 3 / 3 (3) | 1.7 | 1.7 | 0 / 0 |
| client_golang | ce65098 | 3 | 8 / 15 (27) | 10.5 | 10.7 | 0 / 0 |
| client_golang | 745c9bf | 2 | 8 / 15 (27) | 10.3 | 10.8 | 0 / 0 |
| client_golang | c86c247 | 2 | 1 / 15 (27) | 10.0 | 0.6 | 0 / 0 |
| client_golang | b5382cc | 1 | 8 / 15 (27) | 10.2 | 10.3 | 0 / 0 |
| client_golang | 803393d | 2 | 8 / 15 (27) | 10.7 | 10.8 | 0 / 0 |

### What the numbers say

- Heimdall: 5 commits took 106.7 s in full runs and 13.1 s on the selection (8 times less). Commits that touch a leaf
  domain package select 1 package. The commit that changed `internal/domain/change` selects 52 packages, because most
  of the code imports it.
- echo: no gain. The root package is imported by `middleware` and `echotest`, and the root's own tests import
  `middleware`. So a change to the root or to `middleware` selects all 3 test packages. Commit 390f278 touched only
  `middleware/` and still selected 3 of 3 for that reason. The whole suite takes 1.7 s, so there is little to save.
- client_golang: 4 of 5 commits select 8 of 15 packages and save nothing, because the changed package is
  `prometheus` (or `promhttp`), which holds the slow tests. The push-only commit selects 1 package and runs in
  0.6 s against 10.0 s.
- The selection is a ceiling on time saved, not a promise. When the changed package is the heavy, widely imported
  one, selection cannot help. A shared core package is the common case in a small repo and the rare case in a
  monorepo.

### Not verified

- A large repository. None of the three repos is a monorepo: the largest has 111 packages and a 22 s suite. The
  claim in the roadmap (p95 on a large Go repo before and after) is not measured. The 45 minute monorepo figure in
  `CRITIC-deepseek.md` is not reproduced.
- Recall. All 15 commits were green in the full run, so no commit shows a failure that the selection missed. These
  runs cannot show a missed test. The reverse closure is sound for Go imports. It does not see a test that reads
  another package's files, code generation, or a service the test calls.
- One run per cell, no p95, and no repeat. Differences under about 0.5 s are noise (client_golang 10.5 against 10.7).
- Compile time. The build cache was warm, so a cold CI run would add compile time to both columns.
- Other languages. Only Go has a graph adapter. Python, Java, JavaScript and TypeScript run everything until a
  second adapter fills `testselect.Graph`.
- Nested modules. `./...` covers the main module only, so client_golang's `exp` and the tutorials were not run in
  either column.
- The nightly full run is not built here. This package only returns the selection.

## Reviewer runs (P2.1b)

This section records `heimdall-bench review`. For each valid case, the command replays the change as a pull request,
runs the reviewer through `internal/app/agentloop` with a model behind `ports.ModelChat` and five tools (list the
diff, read a file, search, run a test, propose a finding), proves each proposed test with `testintegrity.Prove` (two
runs on the head tree, one on the fixed tree), and scores the run. The scoring rules are in `internal/bench/score.go`.

A live run makes paid model calls. The run holds a hard cap (`-max-cost`, default $3.00): a call whose bound could
pass the cap is never made. The key is read only through `internal/platform/config` and never printed or logged.

## Harness check, fake model

This run proves the harness on two real valid Go cases. The model is not called: `testdata/bench/scripts/*.json`
replays a recorded tool-call sequence, and each turn asserts that the previous tool result held an expected string.
It shows that the tools, the sandbox, the proof rule and the scoring work end to end. It says nothing about what a
real model finds, and it is not a recall result.

| Item | Value |
|---|---|
| Command | `CGO_ENABLED=0 go test -tags benchreplay -count=1 -run 'TestReplayTwoRealCasesWithAScriptedModel' ./cmd/heimdall-bench` |
| Heimdall commit | `56aec92ff014c64bfbfcd6da47edf2bced830a6a` (branch `p2.1b-bench-review`) |
| Started | 2026-11-21T11:12:07Z |
| Machine | darwin/arm64, 8 CPUs (Apple Mac), go1.27.4 |
| Model | scripted fake from `testdata/bench/scripts`, no network call to a model |
| Prices billed | input 0.09, cached input 0.09, output 0.18 USD per million tokens (they only bill the scripted usage) |
| Cost cap | $3.00 for the run, $0.50 per case (neither was reached) |
| Sandbox | local `go test` in a fresh git worktree per run, as the operator's user; the uid sandbox of ACTION_MODE 4.4 is not used locally |
| Exit status | 0: both cases scored |
| Wall time | 107.7 s |

### Results

| Case | Kind | Scored | Stop | Turns | Proposed | Proven | Hit | Located | Tokens in/out | Cost | Time | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| testify-1207 | bug | yes | submitted | 6 | 1 | 1 | yes | yes | 29450/1080 | $0.0028 | 1.9 s | the nil mutex test fails twice on the head and passes on the fix; the finding on `mock/mock.go` 507-511 overlaps the truth 510-511 |
| chi-1185 | bug | yes | submitted | 6 | 1 | 1 | yes | yes | 42600/1130 | $0.0040 | 105.8 s | the discard-and-flush test fails twice on the head and passes on the fix; 2 truth spans moved onto the head tree and 4 fell on lines edited between the head and the fix parent, so those 4 are approximations |

### Summary

| Measure | Value |
|---|---|
| Recall (bug cases with a proven finding on the truth) | 2/2 (100%) |
| False positives (clean cases with a proven blocking finding) | n/a (no clean case ran) |
| Evidence rate (proven / proposed) | 2/2 (100%) |
| Cases scored / not finished | 2 / 0 |
| Cost | $0.0069 total; per case p50 $0.0028, p95 $0.0040 |
| Wall time per case | p50 1.9 s, p95 105.8 s |

### What this check caught

- The chi-1185 case took 105.8 s against 1.9 s for testify-1207, and the difference is the case's own test suite,
  not the harness. The chi `middleware` package takes 25.3 s for one `go test` on this machine, measured twice. The
  proof rule and the `run_test` call each run that suite, so four runs account for the whole number.
- `run_test` cut its test listing at 30 names. In a package with more tests, such as cobra's 213, the reviewer could
  not see the test it had just written, so the clean case looked like a failed run. The focused test now leads the
  list, and a clean case (cobra-1643-clean) scores in its own scripted check.
- The two head runs are separate worktrees, so a flake cannot pass the proof rule by luck.

### Not verified in this check

- Recall, false positives and evidence rate on a real model: this run makes no model call, so these numbers say
  nothing about a model's quality. The live run is the one that fills them.
- The clean case in the fake-model check runs as its own test (`TestReplayACleanCaseWithAScriptedModel`), not in the
  two-case run above, so the table's false-positive row has no value.
- Python, Java, JavaScript and TypeScript cases: `review` takes valid Go cases only, for the same reason `validate`
  does (there is no runner for those languages).

## Progress lines, parallel cases and turn timing (P-2.1c)

A live run was slow and silent: a reasoning model takes 20 to 60 s per turn, a case takes about eight turns, and ten
cases ran in sequence. Nothing was printed until the end. Three changes fix that.

1. **Progress lines.** `review` writes one line to stderr per case start, per model turn, per test run and per case end.
   The writer is shared by the workers and locks itself, so two lines never interleave. The existing `writeDiagnostic`
   writer is reused; no new output path and no `fmt.Print` in product code.
2. **`-parallel N`** (default 1, at most 8) reviews N cases at once. One `bench.CostCap` is shared by every worker. A
   worker now reserves the most its call can cost (`CostCap.Reserve`) and settles the real cost after it, and a call
   that failed settles with zero. The reservation holds until the call settles, so two workers cannot both pass the
   check and together pass the cap. The spend therefore never passes `-max-cost`. Each case keeps its own repository
   cache, sandbox and worktree counter, and the results file keeps the manifest order whatever order the cases end in.
3. **Turn timing.** Every case in the JSON records `turns`, `turn_seconds` (one model latency per answer) and
   `tool_seconds` (one latency per tool call, with its turn and name). The run records `turns` with median and p95 for
   model turns and tool calls, and turns per case. Both appear in the Markdown table.

### Harness check at `-parallel 2`, fake model

| Item | Value |
|---|---|
| Command | `CGO_ENABLED=0 go test -tags benchreplay -count=1 -run 'TestReplayTwoRealCasesInParallelWithAScriptedModel' ./cmd/heimdall-bench` |
| Model | scripted fake from `testdata/bench/scripts`, no network call to a model |
| Cost cap | $3.00 for the run, $0.50 per case (neither was reached) |
| Exit status | 0: both cases scored |
| Case order in JSON | testify-1207, chi-1185 (manifest order, not completion order) |
| Wall time | 107.1 s, against 105.7 s for the same two cases in sequence |

### What the timing says, and what it does not

- The scripted model answers in microseconds, so its model-turn latency is 0.0 s after rounding to the millisecond.
  That number is an artifact of the fake model and says nothing about a live turn. It is recorded so the live run has
  a column to fill.
- The tool latency is real: `run_test` on chi-1185 took 26.3 s on this machine, because that package's suite is slow.
  The two tool-call percentiles separate a fast tool from the one slow call.
- `-parallel 2` saved almost nothing here, because chi-1185's 107 s is one slow `run_test` and the two cases share a
  machine, a disk and a Go build cache. The gain is real on turns that wait on the model; this run waits on `go test`.

### Not verified in this check

- A live model at `-parallel 2`: no live call was made. The parallel proof uses the scripted model only.
- A correctness rate under real concurrency: two cases share one reserve-aware cap by construction, and the unit test
  proves the cap holds across eight workers under the race detector. A run of ten cases at eight workers is not
  measured.
- The model-turn latency of a real reasoning model. Only the scripted model ran.

## Cost estimate for the live run

The live run is all 10 valid Go cases (7 bug and 3 clean) on `deepseek/deepseek-v4-flash` through OpenRouter, at
input 0.09, cached input 0.018 and output 0.18 USD per million tokens (read 2026-11-21).

The fake-model run bills 72,050 input and 2,210 output tokens for two bug cases, but its script makes six short calls
per case. A real reviewer sends the whole conversation on each turn, so the input grows with every turn. The estimate
assumes eight turns per case, a first prompt near 15k tokens and about 6k more per turn, which is about 288k input
and 5k output tokens per case.

| Assumption | Cost for 10 cases |
|---|---|
| 288k input, 5k output per case, no cache reads | $0.27 |
| The same, with 70% of the input read from the cache | $0.13 |
| Cap that stops the run (`-max-cost 3.00`) | $3.00 |

So the expected spend is $0.15 to $0.50, and the hard cap is $3.00. The estimate is `not verified` until a live run
exists: it rests on a token count from a scripted model, not on a real one.
