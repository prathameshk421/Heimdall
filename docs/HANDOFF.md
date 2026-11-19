# Handoff: Heimdall after the H0–H7 overhaul (Nov 18, 2026)

Read this first, then `AGENTS.md`, the top of `docs/PROGRESS.md`, `docs/HARNESS_DESIGN.md` (the D170 spec and the H0–H12 plan), and
`docs/SOTA_QUALITY_PLAN.md` (the quality bar after that plan). `docs/PROGRESS.md` is the evidence ledger; this file is the map and the next actions.

## State in one paragraph

Branch `heimdall-build` (never `main`). H0 to H9 of the harness overhaul, the H10 flow checks and the H11 deploy and live tests flow are committed (reviews, approval and merge wait for owner steps, see below); `make fast` is green
(3,401 tests) and `make verify` passes (3,508 race/component tests). The H10 stories retirement waits for the owner; H11 and H12 are not started. The server runs the reconciler; the old workflow engine is deleted. Nothing ran live: no VM deploy, no
OpenCode/Switch/Jira/GitHub call, no model spend. Every "not verified" line below is deliberate.

Commits, newest first:

| Step | Commit | What landed |
|---|---|---|
| H10 | `e8a8548` | tests and review flow component checks on the real wiring; `run_check` uses the promoted profile; stories not retired (owner) |
| H9 | `H9:` in git log | boot self-check withholds tools, start-up wiring check, runner contract tests, E2 jobs gate priority |
| H8 | `H8:` in git log | profile checks: `run_checks`, `run_check(check_id)`, `leads`, E2 on profile checks, the go vet static path deleted |
| H7 | `506fad5` | kinds, `get_task`, tool table, skills, whole submissions, bundle deleted |
| H6 | `8b2445d`, `633ffe5` | executors record facts only; the gate is computed once; intake and prsync record facts |
| H5 | `5211432` | the reconciler runs the product; `internal/app/engine`, `slice.go`, `timers.go`, `nudge.go`, `workflow.Default` deleted |
| H4 | `5bddf70` | `internal/app/reconcile`: `Run`/`Sweep`/`Loop`, `Recorder`, `Kicking`, the wake/reset/fail ladder |
| H3 | `fd643fb` | store ports and Postgres adapters (`EnsureEffects`, `OpenChanges`, `DeadEffects`), OpenRouter prober, `liveness`/`questions` moved into the domain |
| H2 | `e395b6e` | one row table in `internal/domain/change`; `Missing` derived from it; `wait_pr` gone |
| H1 | `6d0574d` | view fields and four events (`task.superseded`, `task.rewoken`, `provider.probed`, `checks.reported`) |
| H0 | `ed86e10` | design adopted, progress section, old plans marked superseded |

Also in this push: `70ca4d5` allows `encoding/base64` in the domain (owner approved) and points `scripts/check-owned.sh`
at the moved `liveness`/`questions` packages; `docs/harness/golangci.yml` now carries the same base64 line, so
`make harness-check` passes; the accidentally committed server and testagent binaries are removed from the index and
ignored.

The branch was pushed to `origin/heimdall-build` on Nov 18, 2026 (81 commits over the previous remote tip).
`git log -1 origin/heimdall-build` is the docs commit on top of H7 `506fad5`.

## Start here: H12, and the H11 owner steps

H11 (Nov 19 to 20) deployed the overhaul build and ran KAN-26 live: the tests flow works end to end on the VM
(trigger, room, branch, a correct red-first rejection, verified, committed `ab0d649`), and PR #16 got slop, CI,
acceptance, the default pick and three review tasks. Read `docs/LIVE_RUN_H11.md` for the timeline, the cost (about
$0.24, estimated) and defects D1 to D7. Code fixes: `b20e758` (probe the agents' provider, mount `deploy_api_key`,
wait for Graphiti) and `92bf94c` (skills: stop after submitting; join boundary cases to failing ones).

The VM runs server `00a892f2` and runner `c44c04c7`; `/healthz` is `{"status":"ok"}`. Rollback copies are
`/opt/heimdall/heimdall-server.prev`, `/usr/local/bin/heimdall-runner.prev` and `/opt/heimdall/deploy/compose.yaml.prev`
(`ssh heimdall-vm 'sudo sh -s rollback' < scripts/ops/hot-deploy.sh`). Deploy steps are in `docs/RUNBOOK.md`.

Owner steps before re-running the review half of H11:

1. **D6:** run `ssh heimdall-vm 'bash -s' < scripts/ops/install-agent-configs.sh` (it writes the agents' configs and
   tokens, so only the owner runs it). It raises the agents' output limit from 8000 to 32000; then restart the agent
   units. Not verified that Deploy accepts 32000 for this model. The test-author's config (slot 03) is not in the
   installer's list; raise it the same way.
2. **B1:** a promoted profile with a `patterns` check on `aarush323/heimdall-fixture-pay`. No profiler agent exists,
   and approve/promote need a policy owner (`heimdall approve-profile <id>`, `heimdall promote-profile <id>` in chat).
3. **B2:** a `rekdfr` token in the gh keyring (`gh auth login`), so the approval can be made with that login.
4. **D7:** decide whether a ticketless PR is reviewed before slop passes, or the check uses `require_ticket: false`.
5. Then reopen PR #16 (KAN-26) and watch reviews, verify, gate, approval and merge.

## Then, in order

- **H10 stories** (owner decision first): retire `e2e/stories/` with the board that reads it, or keep the board.
- **H11 rest** (owner steps above), then **H12**.
- **H12 Specs and leftovers** (`:599`): rewrite TECH_SPEC 5, 6.2 and 12.4; update RUNBOOK and `doc.go`s; then policy v2
  conversion, `Missing` for an undecodable policy, and the path-redaction fix in `internal/app/record`.

## Owner directive: real-world hardening and SOTA polish

The next owner wants the product proven against the real world, not just unit tests. Use **Opus/Fable** (the strongest
review models available) for deep, adversarial passes, and keep a human in the loop for every live write. The bar is
"state of the art", not "tests pass".

### Real systems on the AWS VM

- `ssh heimdall-vm`; forward ports per `docs/RUNBOOK.md`. Deploy only through H11's script; keep `.prev` binaries and a
  `pg_dump` backup. Roll back by copying `.prev` back.
- **GitHub:** `gh` with the token passed explicitly per call (never rely on the active account). Test logins:
  `aarush323` (PR author) and `rekdfr` (approver). Fixture: `aarush323/PBL_TOS_RISK_ANALYZER` (base `heimdall-demo`) and
  `aarush323/heimdall-fixture-pay` (base `heimdall-test/demo-base`). Test objects only: label `heimdall-test`, PR
  branches `heimdall-test/<run-id>`, product branches `<KEY>-*`. Live PRs target `heimdall-demo`, never `main`.
  Approvals count only from a login that is not the PR author and only on the head commit.
- **Jira:** `.cache/live/jira.sh create` (reads `.env` without printing it); the poll re-queries from `cursor - 10 min`
  and dedupes by `(key, changelog id)`. Verify a ticket edit and a new ticket both start the right change.
- **Switch:** the source of truth for facts is `~/.t3/projects/switchthon/switch`. Slack bridges and room audiences are
  live; chat commands start with the plain word `heimdall` (GitHub uses `/heimdall`). A command counts only when the
  Switch sender id is in `people:`; `sender_kind` cannot tell people from agents, and agents always hold room tools.
- **OpenCode agents:** each VM agent's `opencode.json` holds its token — never read, print or copy it. Model id is
  `openrouter/deepseek/deepseek-v4-flash`. Confirm the agent sync refreshes a cached MCP tool list; if it does not, a
  deploy needs an agent restart (record which).

### Adversarial matrix (run each on the real path and record the evidence)

- Happy path on a real Jira ticket: room opened once, branch created, tests task taken, red-first proof, commit, PR,
  checks, three reviews, E1/E2/E3 verification, gate, human approval, merge. Record minutes per row.
- Ticketless PR; PR for an unknown ticket; two PRs for one ticket; a second push mid-review; a force-push; a closed PR.
- Reviewer submits after the head moved; a stale review; a replayed command; duplicate webhooks; a restart mid-effect.
- A quiet agent, a refusing provider, a dead job, `heimdall resume`; a busy and an unreachable runner; a check that
  writes invalid SARIF; a check that times out; the 5-call budget.
- Untrusted text in the ticket, PR body, diff, check output and room post: it must never become an instruction; the tool
  answer must be wrapped as untrusted.
- Person vs agent commands; a spoofed sender; a command from a room member not in `people:`; two people confirming the
  same proposal; an approval from the author; an approval on an old commit.
- Cross-platform rendering: Slack, Discord, Mattermost, GitHub and the web board — same wording, no ULIDs, findings are
  `F1`, `F2`; threads and edits where the platform supports them.
- Review quality: planted in-diff and out-of-diff bugs, a card-only bug, a clean PR (must stay quiet), a reviewer
  overriding its own finding. Reviews and tests must be competent, not just green.
- Test competence: red-first, flake (3 runs), seam imports, placeholder tests, survivor answers, no test that passes
  without the feature.
- UX: one status message per change rendered from `Missing`; every wait names a person and an action; recovery after a
  restart; no internal ids in chat; timing and noise measured.
- Cost and secrets: state expected spend before every model run; cap $3 for testing; never read, print, log or commit
  `.env`, `secrets/`, tokens or private keys. Live tests get credentials only through the e2e simulator.

### The SOTA quality programme (start after H12)

The owner wants Heimdall proven as a genuinely great, sellable product, not merely a green build. The full brief is
`docs/SOTA_QUALITY_PLAN.md`; the short version:

- Use **Opus/Fable** for the desired-state design, architecture and logic review, feature-mapping review, lens quality
  judgement and the spec. No laziness; the specs and reasoning must be SOTA.
- **Write files first.** For each area, write the best-case desired state (what should happen, what a person sees, what
  the server records, timing and failure behaviour), then compare it with what actually exists and list the gap with
  evidence. Areas: Go server orchestration; agentic reasoning quality; agent triggering on every message and platform;
  whole-product function; lens usefulness; UX; performance and cost.
- **Find root causes in testing**, not symptoms. A failure without the event, tool call, prompt and output that caused
  it is not fixed.
- Do not build a thick layer of unit-test ceremony. Design an **agentic suite run by DeepSeek** (cheap, high token
  budget) that executes the entire end-to-end journey, the adversarial matrix and real-world usage on every pass; Opus
  and Fable design it once the H8–H12 dev plan is complete.
- Ask the owner only for real decisions or access; otherwise decide and record. The quality bar and exit criteria are
  in the plan: a clean real run from a fresh ticket to a merged PR and a ticketless PR, correct agent triggering, SOTA
  reviews, measurably useful lenses, restart-safe orchestration, polished UX and recorded latency/cost.

## Owner decisions already applied in this push

- `encoding/base64` is allowed in `internal/domain`; `base64Of` in `rows_review.go` now uses it directly. The guarded
  template `docs/harness/golangci.yml` carries the same line, so harness drift is gone.
- `scripts/check-owned.sh` points at `internal/domain/liveness` and `internal/domain/questions` (the old app paths are
  gone).
- The baseline `TestMCPGetTaskWithRealAgenttoolsAndFakeTaskIndex` failure is fixed by H7's `get_task` rewrite; the H6
  evidence that still names it is historical.
- Tracked build binaries were removed from the index; both paths are now ignored. The two large blobs remain in the
  H5/H7 history; removing them would need a history rewrite, which the owner has not asked for.
- The nine gitleaks hits were reviewed: a generated SARIF fixture and the deliberately fake AWS example key test
  value in `card_upload_test.go`. Their exact fingerprints are in `.gitleaksignore` with a comment, so the secret scan
  is meaningful again (`make secrets`: no leaks).

## Not verified, and known gaps

- No live run, no model spend, no Switch/Jira/GitHub call in the H0–H7 work.
- The OpenRouter prober only ran against a fake server; the first real probe is one tiny paid request.
- `EffectAgentReset` has no executor (Switch session reset); `EffectRunChecks`'s executor exists only in the H8 patch.
- H11 and H12 are untouched; the H10 stories retirement is not done (owner decision).
- `taskinput` renderers for `profile`, `stack_hints` and `prior_decisions` are still notes; `pr` shows only repository,
  change, base and head (no title/body); `review.submitted.lead_checks` is not set until H8; the no-task answer does
  not say how the last task ended; round-trip tests exist for tests submissions only.
- Agents may hold a stale MCP tool list after a deploy (H11). The boot self-check runs once; a recovered dependency is offered again only after a restart.
- The H6 note stands: without an "already done" check, a crash between a side effect and its recorded fact can repeat
  that side effect once on retry. The fold is idempotent; the side effect may not be.

## Worktrees and branches

- `heimdall-build` is the product branch (this checkout). `main` is 78 commits behind and is not the handoff target.
- Module worktrees (`.t3/worktrees/switch_shi/m*`) are clean. `m0-core` is an old lineage whose work is in the build
  branch as separate commits; `m3-review` and `t3/knowledge-memory-submodule` contain docs-only commits that were not
  merged; read them before porting anything. The dozens of `prunable` `t3-*` worktrees are stale; `git worktree prune` is safe, but leave
  them unless disk space matters.
- `backup/heimdall-build-before-sync` is the pre-sync backup, not product work.

## Commands and traps

`make fast` (format, vet, lint, unit; under 60 s) → `make verify` (component/contract, race, hooks, installer, scripts,
vuln, secrets) → `make e2e T=…` for the real check. `make cost` before an LLM run. `make harness-check` proves the
guarded harness files match `docs/harness/`; edits there need the owner (R2).

- Do not commit a red tree; the pre-commit hook runs the gate. Never `--no-verify`, never force-push, never push unless
  the owner asks (they have asked for this push).
- `make fast | tail` hides the exit status; read the log or run without a pipe.
- The Bash guard hook blocks chained commands that mention lint tooling and regex-heavy shell; run one simple command
  per call and prefix `export PATH=$HOME/.local/go/bin:$PATH`.
- SSH to the VM can stall in key exchange: `-o KexAlgorithms=curve25519-sha256 -o IPQoS=none`.
- Disk has filled before (Docker images). Keep 5 GB free before a build; prune images, not the event store.
