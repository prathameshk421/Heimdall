# Handoff: Heimdall after the H0–H7 overhaul (Nov 18, 2026)

Read this first, then `AGENTS.md`, the top of `docs/PROGRESS.md`, `docs/HARNESS_DESIGN.md` (the D170 spec and the H0–H12 plan), and
`docs/SOTA_QUALITY_PLAN.md` (the quality bar after that plan). `docs/PROGRESS.md` is the evidence ledger; this file is the map and the next actions.

## State in one paragraph

Branch `heimdall-build` (never `main`). H0 to H7 of the harness overhaul are committed and `make fast` is green
(3,375 tests). H8 (checks) was started but is **not committed and does not compile**; the exact diff is saved at
`docs/patches/H8-partial.patch`. H9 to H12 are not started. The server now runs the new reconciler; the old workflow
engine is deleted. Nothing in this session ran live: no VM deploy, no OpenCode/Switch/Jira/GitHub call, no model spend.
Every "not verified" line below is deliberate.

Commits, newest first:

| Step | Commit | What landed |
|---|---|---|
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

## Start here: finish H8

The saved patch is a complete diff against H7 for the files it touches. It is **not** a compiling tree.

```sh
git apply docs/patches/H8-partial.patch
go build ./...   # five errors in three files, listed below
```

What the patch already has:

- `internal/app/effects/checks.go` and tests: one `run_checks` effect, the promoted profile's checks, a baseline run
  when the row asks for one (`SHA: base, Head: head`), only the new SARIF results, per-check `errors` entries.
- `run_check` rewritten: input is `check_id`, the list comes from the profile, unknown id is refused with the list, the
  5-call-per-task budget stays.
- `leads` renderer and the `get_task`/agenttools plumbing that carries check output to the reviewer.
- `finding.ToolResult` uses `check_id` instead of `tool`/`args`; the submit rule and the E2 payload row follow it.

What still has to be written (design H8, `docs/HARNESS_DESIGN.md:547`):

1. Fix the compile sites:
   - `internal/app/review/review.go:195` uses `result.Tool` (now `CheckID`). Replace `Refs.RunExists` (a tool name) with
     the check-id rule: the cited check must be one of the profile's checks offered on this head.
   - `internal/app/effects/verify_evidence.go:211` and `:215` use `p.Check.Name` (now `p.Check.ID`).
   - `cmd/heimdall-testagent/scenarios_review.go:179` builds `Tool`/`Args`; use a real profile `check_id` from the fixture.
2. Move `verify_evidence` E2 onto profile checks. Add `ports.ProfileStore` to `VerifyEvidenceDeps`; load the row by
   `p.Check.ID`; run `ports.SandboxCheck` with `CheckID` (a baseline row runs the base too and filters through
   `sarif.New`). Confirmed E2 = a new result in the same file whose `[Lines]` intersects the finding's range. A
   re-verify with a result still there stays open; no result resolves it. A check that cannot run stays an error so the
   outbox retries; do not turn it into E4.
3. Delete the old static path completely: `ports.SandboxStatic`, `ports.CheckGoVet`, `SandboxJob.Check`,
   `internal/domain/verify/vet.go` and `vet_test.go`, the rootless runner's static branch and tests, and any go-vet text.
   `grep -rn "SandboxStatic\|CheckGoVet\|ParseVet"` must find nothing.
4. Tests the design asks for: baseline + head gives only new results; a failing check becomes an `errors` entry and the
   job finishes; `run_check` unknown id and budget; E2 located confirms, none leaves E4.

Then `make fast`, `make verify`, and update the H8 row in `docs/PROGRESS.md` with the real evidence. Do not mark it done
from this handoff.

## Then, in order

- **H9 Wiring proofs** (`HARNESS_DESIGN.md:562`): `cmd/heimdall-server/selfcheck.go` + test; `CheckKinds` and "every row
  job kind has an executor" at start-up; the runner contract test (client against its handler for `check` and `test`).
  Done when a missing runner logs `run_check withheld: runner unreachable` and a reviewer's tool list lacks `run_check`.
- **H10 Component end-to-end** (`:571`): `tests_flow_component_test.go` and `review_flow_component_test.go` on the real
  wiring with the component fakes and a scripted agent; fake clock, `rec.Sweep` instead of waiting. Retire
  `e2e/stories/` and its Makefile target (owner approval is in Part A of the design).
- **H11 Deploy and live run** (`:580`): state the cost first (DeepSeek v4 flash via OpenRouter, about $0.30–$1 per run;
  cap **$3**), close fixture PRs #13/#14, write `scripts/ops/hot-deploy.sh`, deploy, then run the ticket → room →
  branch → tests → commit → PR → checks → reviews → verify → gate → approval → merge journey and one ticketless PR.
  Record everything in `docs/LIVE_RUN_H11.md`; fix defects in code with a failing test first, never on the VM.
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
- The nine gitleaks hits were reviewed: a generated SARIF fixture and the deliberately fake `AKIAABCDEFGHIJKLMNOP` test
  value in `card_upload_test.go`. Their exact fingerprints are in `.gitleaksignore` with a comment, so the secret scan
  is meaningful again (`make secrets`: no leaks).

## Not verified, and known gaps

- No live run, no model spend, no Switch/Jira/GitHub call in the H0–H7 work.
- The OpenRouter prober only ran against a fake server; the first real probe is one tiny paid request.
- `EffectAgentReset` has no executor (Switch session reset); `EffectRunChecks`'s executor exists only in the H8 patch.
- H8 is incomplete (above). H9–H12 are untouched.
- `taskinput` renderers for `profile`, `stack_hints` and `prior_decisions` are still notes; `pr` shows only repository,
  change, base and head (no title/body); `review.submitted.lead_checks` is not set until H8; the no-task answer does
  not say how the last task ended; round-trip tests exist for tests submissions only.
- `CheckKinds` runs in a test, not at server start (H9); agents may hold a stale MCP tool list after a deploy (H9/H11).
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
