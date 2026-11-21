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

## t3code cases (P2.1d)

This section records `heimdall-bench validate` on 15 cases from pingdotgg/t3code, a TypeScript and Rust monorepo of
about 1 GB. The cases are at the end of `testdata/bench/cases.yaml`. All GitHub data came from GET calls. The bench
fetches the commits from the owner's fork `prathameshk421/t3code`, synced once with `gh repo sync`. Nothing was
pushed, opened or commented anywhere.

### How a cargo or vitest case runs

- The case names its toolchain (`cargo` or `vitest`), its workdir, its install steps, sparse-checkout patterns and
  the fix's tests. A TypeScript case checks out only the root files, `patches/`, every `package.json`, `scripts/`,
  `infra/task-cache/` and the packages it needs (24 MB at HEAD for `packages/shared`). A Rust case checks out only
  `.cargo/` and its crate.
- The tree has no `.git` link to the cache. So pnpm's `prepare` script (`vp config`, which writes git config) cannot
  change the cache, and a test cannot read later history.
- One tree serves both runs: the fix parent with the fix's tests put in (before), then the same tree with the fix's
  files written over it (after). Install runs once per tree, and again only when the fix changes a manifest or a
  lockfile. Install time and test time are timed apart.
- pnpm's store, `CARGO_HOME` and `CARGO_TARGET_DIR` live under the bench cache in the OS temp dir.
- Rust unit tests live inside the source file. The before tree takes the fix's `#[cfg(test)]` module spliced onto
  its own code, not the fix's whole file. Truth leaves that module out.
- Vitest does not type-check. So for each red test I read the first failure line in the results file: every one is
  an assertion or a runtime error of the bug (below), and none is "is not a function".
- The intro (`change`) is chosen by blame: the newest commit that last touched the fix's old-side source lines.

### Run

| Item | Value |
|---|---|
| Command | `CGO_ENABLED=0 heimdall-bench validate -only <the 15 t3code ids> -out final.json` |
| Heimdall tree | `ca9c269` (branch `p2.1d-t3code-cases`) plus the uncommitted P2.1d step 3 and 4 code, committed right after |
| Started | 2026-11-21T14:38:08Z |
| Machine | darwin/arm64, 8 CPUs (Apple Mac), go1.27.4, node 26.13.1, pnpm 12.21.0 (each tree's `packageManager` pnpm ran the install), cargo 1.99.1, network on |
| Exit status | 0: every case agrees with its manifest status |
| Wall time | 178.4 s for 15 cases, with warm pnpm and cargo caches |

The pnpm store was warm from earlier runs on the same day. A cold install of `packages/shared` at t3code HEAD took
191.6 s (3m11.6s, 312 packages downloaded). The first validate run of these cases, with a partly warm store, spent
23 to 27 s per install. Each status held on two runs: this recorded run and one full run of the same case before it.

### Results

Times are in seconds. "Targets" is the fix's tests the manifest names, and "Red" is how many of them fail before the
fix. Every target passes after the fix in each valid case.

| Case | Lang | Kind | Outcome | Install | Tests | Total | Targets | Red | First failure line before the fix | Intro evidence |
|---|---|---|---|---|---|---|---|---|---|---|
| t3code-5025 | typescript | bug | valid | 5.3 | 1.7 | 8.4 | 1 | 1 | `AssertionError: expected { note: 'a]' } to deeply equal { note: 'a,]' }` | intro has no pnpm-lock.yaml, not run |
| t3code-6189 | typescript | bug | valid | 4.5 | 1.6 | 19.2 | 1 | 1 | `AssertionError: expected 'C:' to be 'C:\'` | red at the intro, red at its parent too |
| t3code-6175 | typescript | bug | valid | 4.8 | 1.7 | 7.8 | 3 | 1 | `AssertionError: expected 'github' to be 'unknown'` | intro has no pnpm-lock.yaml, not run |
| t3code-6670 | typescript | bug | valid | 5.7 | 2.4 | 23.1 | 2 | 1 | `RangeError: Invalid time zone specified: Etc/Unknown` | the fix's tests do not load at the intro |
| t3code-12537 | typescript | bug | valid | 6.0 | 3.1 | 25.2 | 1 | 1 | `AssertionError: expected { kind: 'gitlab', …(2) } to deeply equal { kind: 'gitlab', …(2) }` | red at the intro, red at its parent too |
| t3code-11425 | typescript | bug | valid | 5.9 | 2.2 | 24.1 | 6 | 6 | `AssertionError: expected [] to deeply equal [ [ '' ] ]` and two more of the same kind | red at the intro; the tests do not load at its parent |
| t3code-12763 | typescript | bug | valid | 4.1 | 2.0 | 7.5 | 1 | 1 | `SchemaError: Missing key` | intro has no pnpm-lock.yaml, not run |
| t3code-32169 | typescript | bug | valid | 3.8 | 1.7 | 7.2 | 1 | 1 | `AssertionError: expected 1 to be less than 0` | intro has no pnpm-lock.yaml, not run |
| t3code-38193 | typescript | bug | valid | 1.2 | 1.3 | 17.4 | 1 | 1 | `AssertionError: expected [ '--add-dir', '/tmp/project', …(2) ] to deeply equal [ '--add-dir', …(2) ]` | red at the intro, red at its parent too |
| t3code-39217 | typescript | bug | valid | 2.2 | 1.3 | 12.4 | 7 | 3 | `AssertionError: expected true to be false` (and the reverse) | red at the intro, red at its parent too |
| t3code-19745 | rust | bug | valid | 0.2 | 2.9 | 4.1 | 1 | 1 | `assertion failed: unselected.cmd().is_empty()` | the fix's tests do not build at the intro |
| t3code-23426 | rust | bug | invalid | 0.1 | 1.0 | 1.4 | 1 | 0 | the crate does not build on macOS (below) | |
| t3code-22116 | rust | bug | invalid | 0.0 | 0.7 | 1.1 | 3 | 0 | the crate does not build on macOS (below) | |
| t3code-18289-clean | typescript | clean | valid | 4.8 | 3.7 | 10.1 | | | `vp test run` passes at the merge, 352 tests | |
| t3code-10008-clean | typescript | clean | valid | 4.3 | 3.7 | 9.0 | | | `vp test run` passes at the merge, 508 tests | |

"Total" also holds the fetch, the checkout and the intro evidence runs, which install again in their own trees.

### Counts

| Language | Valid bug | Valid clean | Invalid | p50 install | p50 tests |
|---|---|---|---|---|---|
| typescript | 10 | 2 | 0 | 4.7 s | 1.9 s |
| rust | 1 | 0 | 2 (bug) | 0.1 s | 1.0 s |

### What the run showed

- The two Hyprland cases are invalid on this Mac. `smithay-client-toolkit` 0.19.2 imports `rustix::pipe::pipe_with`,
  which rustix leaves out on Apple targets, so the crate does not build after the fix either. Their tests also call
  functions the fix adds (`next_event`, `captured_metadata`), so on Linux they would still be a build error before the
  fix. t3code has only three Rust crates and 10 `.rs` files. Only three 2026 Rust fix PRs add a test, so Rust stays
  small.
- `it.each` and `describe.each` format their titles (`with delimiter %j` becomes `with delimiter ","`). So a vitest
  target must be named as the report names it, and the validator checks only that its file is one the fix changed.
  Two rows of t3code-39217 have a CR LF inside the title. The manifest keeps a test id on one line, so those two rows
  are not targets.
- Blame picks a weak intro. In 4 of the 5 cases where the fix's tests run at the intro, they also fail at the intro's
  parent, so the bug is older than the blamed commit. In 4 more cases the intro is older than the pnpm lockfile.
  A replay review uses the intro as the head, so these cases still show the bug at the head, but the intro is not
  the commit that brought it in.
- Older trees need more of the repo: in July to September 2026 the root `prepare` script ran `scripts/` files. The
  sparse patterns take the whole `scripts/` directory for that reason.
- `prepare` runs `vp config`, which writes `core.hookspath` into the git config of the repository it finds. A plain
  tree has no `.git`, so the cache stays clean.

### Clean cases

Both clean PRs have no cross-reference from a fix or revert in their GitHub timeline (GET, 2026-11-21), and no later
commit whose subject starts with `fix` touched their source file up to t3code HEAD `e0f3ab9e7`. #10008 removes an
unused predicate, so it is a weak false-positive probe. #18289 changes how the GitHub client reads credentials.

### Not verified

- Linux. The Hyprland and KDE crates need a Linux runner. I did not run them on the VM.
- The intro of each bug case: blame is a heuristic, and no case's intro evidence is confirmed.
- Flakiness beyond the runs above: each verdict held on at least two runs, never more than three.
- Cold-cache times for each case. The table has warm-store times; the one cold install measured is in "Run".
- Type errors. Vitest strips types and does not check them, so a test that is red only because of a type error
  cannot happen, but a type error the fix brings in is not seen either.
