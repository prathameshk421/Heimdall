# Heimdall progress

Every session reads this first and updates it before stopping. Status per task is in `TRACKER.md`.

## State (Nov 4, 2026, end of session 2)

Branch `heimdall-build`, nothing pushed. **Done and committed:** Stage 1, slice 0, and now **T11** (frozen interfaces as Go: `finding`, `deliberation`, `testset`, `workflow`, `policy`, `slop`, `gate`, `change`, `ports`, `schema`), **T31 built** (fixtures `pay-demo`, `ledger-demo`, `internal/testkit/fixtures`; not pushed to GitHub), the **slice-0 review fixes** (shared test guard, `id`, recorder, honest board) and the **T16 hardening** (stricter lint, hooks, CI, the harness is now **installed**). `make fast` is green (950 tests). `make verify` is green except `secrets` (see "Waiting on the human"). The Sonnet slice-0 review is done: no Critical findings; its High and Medium items are fixed except those listed under "Not verified yet".

## Session 3 in progress (Nov 4): slice 1 "the door"

- **T17** built by a Haiku helper, reviewed, not committed yet: `internal/platform/config` (strict TOML, `[[projects]]`, `Secret` via `os.OpenRoot`, `OnceSet`, `FilePath`), `transport.NewRouter` with `/healthz`, `cmd/heimdall-server` `run()`. Evidence: 55 tests pass, lint 0 issues on the three packages, 8 mutations caught (helper's report; I re-ran the tests only). Names differ from the brief: `FilePath` (revive stutter), `run` takes a listen func, router takes a logger. Not verified: a real SIGINT/SIGTERM, a laptop run against a real `secrets/` (`make secrets-local` does not exist).
- **T21** built by a Haiku helper, reviewed, not committed yet: `internal/domain/change/reducer.go` (`Reduce`, `Replay`, `ErrIllegalTransition`, table built in a function). Design change: `tests.verified` now carries `needs_signoff` (spec 3.2 updated); true goes to AWAITING_TEST_SIGNOFF, false to TESTS_READY, and `tests.committed` from AWAITING_TEST_SIGNOFF is illegal. Mutations caught. Choices: `pr.linked` from UNLINKED_PR goes to REJECTED (closes the stream, no new State); `pr.closed` unmerged goes to REJECTED. Not verified: invariant 2 (the reducer keeps no head sha).
- **T20** done: `internal/adapters/postgres` (goose migrations, `Store` for EventStore and Outbox, cursor, webhook dedupe, `VerifyAll`). Evidence: `make fast` exit 0 (1885 tests, lint 0), `make verify` exit 0, helper ran 37 component tests (also with `-race`) and 12 mutations caught. Spec 4.1 now has `outbox.target`. Effects are attributed to the last event of an append. `occurred_at` is cut to microseconds before hashing. Not verified: process-level `kill -9`; the advisory lock is only caught by the backend-terminate test (UNIQUE gives the same outcome on a race).
- T17 is committed as b3d05be and T21 as 928cd1a, T20 as 41a0f4d.
- **T22** in progress (helper): `ports.Outbox.Claim` with a lease (migration 0002), `internal/app/outbox` drainer. Drainer first; River is not added yet (the network works, so it can be). Not reviewed or committed.
- **T13** built (helper), not committed: `internal/adapters/registry` and provider selection in config. The helper did not see a clean red for the registry package (implementation written before its first test run); 9 mutations were caught instead. The secret-leak test guards only a test stub, so each real adapter must run it. The T14 tracker row says status "error", the registry says "unreachable": reconcile in T14.
- **T12** partial (helper stopped after repeated failures; the owner said Graphiti is not deferred): `deploy/` has compose (server, Postgres, FalkorDB, Graphiti `zepai/knowledge-graph-mcp:1.1.3-graphiti-0.31.2-standalone`, Ollama profile), Dockerfile, example config, `verify-compose.sh`, and a `doctor` stub in `cmd/heimdall`. The real `deploy/verify-compose.sh` run is in progress; until it passes T12 is not done. Not verified: Graphiti healthy with the OpenRouter embedder config.
- `go mod tidy` was run (go-toml/v2, goose/v3 added; network works). River is not added yet.
- **T14, T24, T46, T32, T25** built (Haiku and Sonnet helpers, finished and checked by the lead), committed together. Evidence: `make fast` exit 0 (2246 tests, lint 0 issues). `connect` CLI with `file` tracker entry and `lineWriter` error handling; GitHub adapter (`internal/adapters/github`: PR reads, ETag list, rate limit and breaker, narrowed token source, branch/commit/check/comment writes, `ReadFile`/`TreeHash`/`Tarball`); intake (`internal/app/intake`) with the YAML decoder injected from `internal/adapters/policyyaml` (depguard bars YAML from `app`); `internal/app/commands` parser; `internal/adapters/tracker/{file,jira}`. Port changes: `CodeReader.ReadFile`, `ports.ErrUnauthorized`; the reducer now allows `slop.evaluated` before BUILDING. Mutation checks: 4 to 5 per task, all caught (one equivalent mutant simplified in the GitHub branch code). Fixture repo `aarush323/heimdall-fixture-pay` published (5 branches).
- **Not verified:** no live GitHub, Jira or Switch call (credentials go only through the e2e simulator, R8); T35 `doctor` reading real installation permissions; T04 spike (required check blocks merge); Jira `search/jql` and changelog behaviour from docs only; `make verify` not run. **T12 done:** `deploy/verify-compose.sh` ran to completion (Nov 4): `compose config -q`, `up -d --build --wait` exit 0 with postgres, falkordb, graphiti and heimdall-server all Healthy, `/healthz` on 127.0.0.1:8080 exit 0, `down -v` clean. Still not verified: Graphiti answering a real search with the OpenRouter embedder config.
- **Left for T36:** KAN golden ADF tests, Jira breaker and retries, the trigger decision that turns a changed ticket into `change.triggered`.

## Next (in order)

1. The owner decisions below (they unblock a green `make verify`).
2. T17 (config + secrets loader + `/healthz`; `pelletier/go-toml/v2` is available), T12 (compose skeleton), T13 (registry), T20/T21/T22 (event store, reducer, outbox; `change` and `ports` already define the events, hash chain, effect keys and `EventStore`), T24/T35/T46, T14, T04 (spike; needs the fixture repo pushed). Order is `docs/MVP_SCENARIOS.md` section 7, slice 1.
3. Publish the fixture repo (write `make fixtures-push`; repo `aarush323/heimdall-fixture-pay`, base `main`), then remove `pending: T31` from `e2e/stories/fixture-pay.yaml` and fill the planted files and lines from `testdata/repos/pay-demo/manifest.yaml` (the story is guarded: ask).
4. Run the CI workflow once on GitHub (PBL repo, a `heimdall-test/` branch). Not verified yet.
5. The next architecture review (Opus only when the owner says usage is back, otherwise Sonnet) after the event store and reducer exist, then at each slice end.

## How to resume

Work in this checkout (`.env` is gitignored). Go is not on a non-login shell's PATH: `export PATH=$HOME/.local/go/bin:$PATH` (the Makefile does it too). Commands: `make fast` (about 10 s), `make verify` (about 1 min, needs Docker), `make lint`, `make lint-selftest`, `make harness-test`, `make harness-check`, `make prototype [TASK=T##]`, `make demo-check`, `make e2e T=T##`. The pre-commit hook runs gitleaks on the staged diff and then `make fast`. VM: `ssh heimdall-vm`; ports in `docs/RUNBOOK.md`. Commit `T##: what`, end with the `Co-Authored-By` line from the system reminder. Do not push without being asked.

## Rules learned (do not relearn)

- The owner wants continuous work, real validation (red first, mutation checks, `not verified` written honestly), and bulk code delegated to **Haiku helpers at medium effort**: one common preamble file with the rules, then one package group per helper with disjoint directories, helpers never commit, and then my own review, mutation and lint before committing. Opus only for architecture reviews once a good part of the product exists. D162: nothing is cut.
- Helpers hit the session limit once; `SendMessage` to the same agent id resumes it with its work intact.
- Product code goes in top-level folders (`cmd/`, `internal/`, `e2e/`), never under `docs/`. Nothing demo-specific goes in product code.
- Never print or read `.env`, `secrets/`, tokens. The classifier blocks reading credentials.
- Lint is strict and finds real bugs every time; fix, never suppress. `gochecknoglobals` allows only `regexp.MustCompile` vars. depguard matches by prefix, so allowing `io` also allows `io/fs`; deny entries are needed.
- gitleaks scans history: fake secrets in tests must not look like secrets (split them, or use neutral values). The installed hook now runs gitleaks on the staged diff.
- **The installed hooks guard `docs/harness`, `.claude`, the scripts, `Makefile`, the lint config, archcheck, lintselftest and `testdata/lint`.** The template `docs/harness/golangci.yml` is the source of `.golangci.yml` (`make harness-install` rewrites it): change the template, then reinstall, never only the installed copy. The bash guard has false positives (see below): keep redirects and guarded names out of one command line, and use Write or Edit for file changes.
- `go list` exits 0 for a package that did not load; always check its `Error` fields.
- OpenCode `edit` permission pattern is `**/scratch/**`. A pending `question` tool call hangs a session forever. `auto_mode: true` is required.

## Not verified yet

- Nothing in slice 1 has run. The scenario step bodies beyond those with behaviour tests first run when their drivers land (Jira T36, GitHub T31, chat T26).
- The CI workflow has never run on GitHub. Graphiti, River, goose, the GitHub App token path, and Mattermost bridge commands beyond T01-T03.
- Schemas were not checked against a real JSON Schema validator (none in `go.sum`). Limits the spec left open (reviewers.count 1 to 3, max_rounds 1 to 3, the slop justification heading `Justification`, the workflow maximum timeout of 24h) are choices, listed in each package's `doc.go`.
- `make cost` and `make fixtures-push` exit 1 on purpose until T19 and T31 publish.
- Review items not fixed yet: M2 (archcheck checks only direct imports, default tags and five layers), `StatusManual` is unreachable (decision below), there is no lint rule against `go func` in domain, and forbidigo cannot tell `fmt.Fprintln(os.Stdout)` from a file write (`os.Stdout` itself is forbidden now).

## Waiting on the human

1. ~~`make verify` red on gitleaks~~ **Done** (owner said yes): five fingerprints are in `.gitleaksignore`, `make verify` exits 0 (Nov 4, commit `d1ad813`).
2. **Apply the bash-guard fix.** The installed guard (`pre-bash-guard.sh`) refuses harmless commands: a `2>&1` or other redirect plus a later mention of a guarded file in one command line; any command containing `make secrets`; `s[...` followed by a slash. The owner said yes, but the installed permission settings deny edits under `docs/harness` and I will not work around that. A tested fix (129 hook cases green in a scratch copy) is the patch `docs/patches/bash-guard-fix.patch`. **The owner applies it:** `git apply docs/patches/bash-guard-fix.patch && make harness-install && make harness-check`, then deletes the patch file. Until then, keep redirects and guarded names out of one command line, avoid the word `secrets` after `make` (run `go tool -modfile=tools.mod gitleaks ...` directly), and use Write or Edit for files.
3. **`StatusManual`** (`e2e`): `MVP_SCENARIOS.md` line 57 says a manual step shows `manual`, but the guarded test `e2e/scenarios/spec_test.go:92` requires every unbuilt step, P9.3 included, to be `blocked`. Pick one: change the test, or fix the doc and keep it blocked.
4. `-strict` now fails whenever a step's cost was not measured (helper E's choice, stricter than the brief). Confirm.
5. `ana` in the fixture's `heimdall.yaml` has the placeholder GitHub login `ana-fixture` (the policy requires one for approvers). It is not a real account; tell me if you want a real one.

## Failing tests

None committed. `make fast` is green at HEAD.

## Spend

OpenRouter: $0.149 of the $3 testing cap, read Nov 4 after T03. No live model run since. The Haiku helpers and one Sonnet review ran on the Claude plan; no OpenRouter spend.

## Noticed

- `workflow/task.go` has its own `validateFiles`; `finding.CheckRepoPath` does the same job. Consolidate (T11 follow-up).
- The hash chain is per change (spec 4.2 amended). A truncated tail cannot be seen from the chain alone: the nightly job must publish per-change heads.
- Gate: an empty review-request list passes the reviews clause; the caller must leave `ReviewsKnown` false until reviewers are picked (spec invariant 7).
- `TestsVerified.NeedsSignoff` is a plain `bool`: a producer that omits it silently skips sign-off. Only the engine produces it; make it required (`*bool`) if another producer appears.
- `Makefile` target `e2e`/`cost`: `TODO(T19)` marks where the budget guard goes.

## Board history

| Date | Task | Story / agents | Green / blocked / red |
|---|---|---|---|
| 2026-11-04 | T98 | fixture-pay / scripted | 0 / 38 / 2 |
