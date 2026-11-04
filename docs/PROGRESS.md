# Heimdall progress

Every session reads this first and updates it before stopping. Status per task is in `TRACKER.md`.

## State (Nov 4, 2026, end of session 2)

Branch `heimdall-build`, nothing pushed. **Done and committed:** Stage 1, slice 0, and now **T11** (frozen interfaces as Go: `finding`, `deliberation`, `testset`, `workflow`, `policy`, `slop`, `gate`, `change`, `ports`, `schema`), **T31 built** (fixtures `pay-demo`, `ledger-demo`, `internal/testkit/fixtures`; not pushed to GitHub), the **slice-0 review fixes** (shared test guard, `id`, recorder, honest board) and the **T16 hardening** (stricter lint, hooks, CI, the harness is now **installed**). `make fast` is green (950 tests). `make verify` is green except `secrets` (see "Waiting on the human"). The Sonnet slice-0 review is done: no Critical findings; its High and Medium items are fixed except those listed under "Not verified yet".

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

1. **`make verify` is red on `secrets`**: gitleaks reports four findings in already-committed history (fake values in test files at commits `32a0472`, `c232ccd`, `05995ff`). History cannot be rewritten (R11), so each needs a fingerprint line in `.gitleaksignore` (a guarded file). The working tree no longer contains them. Say yes and I add the four lines (or add them yourself).
2. **A false positive in the installed bash guard** (`docs/harness/claude/hooks/pre-bash-guard.sh`): the guarded-file check scans the whole command line, so a command with `2>&1` and a later mention of a guarded file is refused; the new secrets-glob rule also fires on `s[` followed by a slash later in a command. The fix is to look only at a redirect or write verb followed directly by the path within one command segment, with test cases. It needs an edit of `docs/harness`, which the guard reserves for you. Say yes to lift it for that one file, or apply it yourself.
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
- `Makefile` target `e2e`/`cost`: `TODO(T19)` marks where the budget guard goes.

## Board history

| Date | Task | Story / agents | Green / blocked / red |
|---|---|---|---|
| 2026-11-04 | T98 | fixture-pay / scripted | 0 / 38 / 2 |
