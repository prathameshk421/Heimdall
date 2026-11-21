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
