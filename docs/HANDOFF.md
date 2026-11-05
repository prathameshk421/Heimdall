# Handoff: how to continue Heimdall (written by Claude, Nov 4 2026)

If you are a new session, read this file first, then `docs/PROGRESS.md`, `CLAUDE.md`, and the task row in `docs/IMPLEMENTATION_PLAN.md`.

## State
- Branch `heimdall-build`, pushed to `origin/heimdall-build`. HEAD was `092b72c` when this was written.
- Committed and verified (`make fast` green, 2246 tests): T10, T11, T12 (compose verified), T13, T14, T16 to T22, T24 (GitHub adapter), T25 (file + Jira tracker), T32 (commands parser), T46 (slop intake).
- Added this session: `ports.CodeReader.ReadFile`, `ports.ErrUnauthorized`, `internal/adapters/policyyaml` (YAML decode injected into intake because depguard bars YAML from `app`), reducer now allows `slop.evaluated` before BUILDING.
- Fixture repo `aarush323/heimdall-fixture-pay` is published with 5 branches.
- In flight when this was written (Haiku helpers, uncommitted work may exist in the tree): T23 (step engine), T26 (Switch client), T28 (MCP server), T36 (Jira ADF + breaker). Check `git status` before doing anything; finish or verify each, run `make fast`, then commit.

## Not verified anywhere
No live GitHub, Jira or Switch call has run. T04 (GitHub App check that branch protection requires) is open and needs live credentials through the e2e simulator only. T35's `doctor` reading the real installation permissions is not built. `make verify` has not been run on the new packages.

## How the owner wants you to work (hard rules)
- The owner is impatient. Do not ask small questions; decide, state it in one line, and go. Short replies. Do not narrate.
- Use Haiku (`model: "haiku"`) for every subagent. Never the default model. Parallelise unblocked tasks.
- Commit once, when a batch is done and `make fast` is green, then push `heimdall-build` (the owner asked for push). Never `--no-verify`, never force-push. The pre-commit hook runs the whole gate and rejects red trees, so do not commit while a helper's red-stage tests are in the tree.
- Never read secrets (R8). Live tests get credentials only through the e2e simulator.
- No `//nolint`, no edits to `.golangci.yml` or the e2e scenarios (R2). If lint complains, restructure the code.
- Helpers' reports are not proof: re-run tests and lint yourself (`go test -count=1`, `go tool -modfile=tools.mod golangci-lint run ./pkg/...`) before committing. Twice a helper claimed done with lint failures.

## Practical traps
- The Bash guard hook blocks chained commands (`&&`, `;`) that mention lint tooling and regex-heavy shell. Run one simple command per call; prefix `export PATH=$HOME/.local/go/bin:$PATH`.
- `make fast | tail` hides make's exit status; read the output file or run it without a pipe.
- Disk got full once (Docker images, 13 GB, pruned). Check `df -h /` before big builds; keep 5 GB free.
- Stray untracked binary `heimdall` in the repo root: never commit it.
- Edit the progress evidence in `docs/PROGRESS.md` for each finished task (commands, results, what is not verified).

## Update: T04 is done (live)
The owner has a working `gh` login (accounts `aarush323` with repo scope, and `rekdfr`). T04 ran live: see `docs/PROGRESS.md`. Branch protection on the fixture repo's `main` requires `heimdall/gate`; PR #1 is open and blocked on purpose. The Bash guard hook still blocks `.env` reads and live writes outside `heimdall-test` branches; when the owner authorises such a step, run the one command through `mcp__terminal__run_in_terminal` (the owner approves it) and never print secret values. Live tests that need the App key read `.env` inside the Go process (see `internal/adapters/github/live_test.go`, build tag `live`).

## Low-complexity, huge-feature tasks a weaker model can do
These are big in volume but need little judgement: a clear spec, local tests only, no live systems, no cross-package design. Give each to a Haiku-class model with the task row, `CLAUDE.md` and one finished package as the style example (`internal/adapters/tracker/jira` or `internal/adapters/github`).
- **Skills as markdown (T40 reviewer, T41 resolver, T34 profiler skill):** long `SKILL.md` files with schemas to follow. Test against the sample inputs in the spec. No Go design.
- **Fixture matrix (T39):** hermetic Go, Python, Node and JVM repos in `testdata/repos/` with known red and green tests. Pure file generation plus a test that every branch builds, copied from the existing `testkit/fixtures` tests.
- **Scripted test agent (T38, `cmd/heimdall-testagent`):** canned valid and invalid outputs over the MCP protocol. Mechanical once T28 exists.
- **JUnit parser and red-first/flake checks (part of T29):** parse XML, decide red/green/flaky. Table tests only.
- **Detectors for the repo profile (part of T34):** find `go.mod`, `package.json`, `pom.xml` and emit a profile. Pure functions over a file tree.
- **People mapping (T37):** the `people:` table keyed by Switch sender id and a `connect people` subcommand. Extend `cmd/heimdall/connect.go` using its existing tests as the pattern.
- **Remaining small adapters and parsers:** AC parsing variants, ADF node types, config keys, doctor checks. Each is a table of inputs and expected outputs.
- **Docs and evidence:** filling `docs/PROGRESS.md`, `doc.go` design notes, golden files.
Keep the judgement-heavy work (workflow engine, reducer changes, gate and merge rules, deliberation, memory design, security boundaries) with the stronger model.

## Next tasks (in rough order)
T04 (needs live creds), T35 doctor permissions, T23/T26/T28/T36 (in flight), then T27 (agent runtime), T29 sandbox, T30 test-author flow, T34 repo profile, T37 people mapping, T38 scripted agent, T40 to T44 reviewer flow, then the review/deliberation/merge loop, memory, shipping. See the plan table.
