# Heimdall: test and validation strategy

Status: v1.1, Nov 4, 2026 (decisions D111 to D120 in `BRAINSTORM_BACKLOG.md` Part 13, amended by Part 16). Applies to every task in `IMPLEMENTATION_PLAN.md`. `STAGE_1_FIXES.md` runs before the first task and has its own checks.

**Who builds this.** Claude Code builds Heimdall incrementally over many sessions, one task at a time. A model that loses context between sessions and tends to believe its own work needs two things from the process: tests that remember what the code must do, and checks it cannot talk itself out of. This document makes both mandatory.

**The rule that matters most: a task is done only when it passes against the real systems, with the evidence written down.** Test doubles exist to make regression runs fast, free and repeatable. They never replace the real check, and every double is itself checked against reality.

**Product first.** The layers below prove each part works. `MVP_SCENARIOS.md` proves the parts add up to the prototype: nine scenarios (P1 to P9) driven from the outside, one progress board (`make prototype`), product-quality checks on every transcript, usefulness tests (context ablation, baselines, wrong-implementation kill) and the demo script as a test (`make demo-check`). Each task in the plan names the board step it turns green, and that step, not the task's own tests, is what makes it done.

## 1. Principles

1. **Red first.** Every behaviour starts as a failing test that fails for the right reason (an assertion, not a compile error or a missing file). The failure output is read before any code is written.
2. **Real first.** A feature is not done until it has run on the real stack it touches: real Postgres, real Docker runner, real GitHub App, real Jira (`KAN`), real Switch and Mattermost, real agents. Fakes are test doubles and are never product adapters (this keeps the team's "everything real" rule; see D112).
3. **Cheap layers first, expensive layers last.** Most bugs die in layers that cost nothing. Language-model calls happen only in the layers that need them, with a hard budget.
4. **Doubles are recorded from reality.** Fixtures come from real traffic (scrubbed), and a drift check compares them with the live systems. A double that nobody checks is a lie.
5. **Evidence, not claims.** Done means a command, its exit status, and the key lines of its output are recorded in the tracker. "Should work" and "not run" are written as such.
6. **Fail closed in tests too.** A skipped test, a missing fixture or an exhausted budget fails the run; it never passes silently.

## 2. The layers

| Layer | What | Needs | LLM cost | When it runs |
|---|---|---|---|---|
| **L0 Static** | `gofmt`, `go vet`, `staticcheck`, `golangci-lint`, dependency-rule check, `gitleaks`, schema compile | nothing | $0 | every save and commit (`make fast`) |
| **L1 Unit, property, fuzz** | pure domain (reducer, merge rule, slop, tiering), table tests for every state and event pair, `testing/quick` replay property, Go fuzzing for parsers (ADF, JUnit, command grammar, redactor) | nothing | $0 | `make fast` |
| **L2 Component** | adapters against real Postgres (testcontainers), the real runner and Docker sandbox on fixture repos, the real HTTP server with `httptest` clients | Docker | $0 | `make verify` |
| **L3 Contract** | parsers and adapters against **recorded real payloads** (`testdata/real/`: GitHub, Jira ADF, Switch, Mattermost, JUnit from pytest, go test, jest); MCP schemas and golden JSON | recorded files | $0 | `make verify` |
| **L3b Drift** | read-only calls to the live systems compared with the fixtures' shape | network, tokens | $0 | `make drift` (before each gate, and nightly) |
| **L4 Real integration, no LLM** | the real GitHub App (check runs, narrowed tokens), real Jira KAN, real Switch + Mattermost, with a **scripted agent**: a deterministic process that speaks the real agent protocol and returns canned, schema-valid or deliberately bad submissions | accounts, VM for Switch | $0 | `make e2e T=<id>`, and at each gate |
| **L5 Live LLM smoke** | one real OpenCode agent per role on a tiny fixture task; asserts structure and outcome, not wording | VM, key budget | about $0.03 to $0.04 per run | when a task changes an LLM-facing piece, and at gates |
| **L6 Evals** | small curated corpora (injection, reviewer, test-author, resolver) with ship bars | VM, key budget | see section 4 | before a skill version change and at G3 and G4 |
| **L7 Rehearsal and soak** | the full demo story on the Jurist AI target; the planted-issue check (each planted issue found with evidence the code verified, in at least 2 of 3 runs); a 2-hour soak with scripted agents (LLM off) | everything | about $0.15 to $0.20 per full run; soak $0 | G4 and before recording |

**Make targets** (T18 builds them):
- `make fast`: L0 and L1, under 60 seconds. Runs in the pre-commit hook.
- `make verify`: adds L2 and L3, under about 5 minutes. Runs in CI on every push.
- `make drift`: L3b. `make e2e T=T24`: L4 (and L5 when the task is LLM-facing) for one task, after a budget check. Each task's check is a Go test named `TestTask_T24` in `e2e/` (build tag `e2e`). A task with no such test fails the run, so a missing check can never pass.
- `make harness-test`: every hook case in `STAGE_1_FIXES.md`. `make secrets-local`: fills the gitignored `secrets/` from `.env` (D148). `make fixtures-push`: publishes `testdata/repos/pay-demo` (D149). Both run before any local-world check.
- `make eval SUITE=injection`: L6 with a budget check. `make rehearse`: L7. `make soak`: the 2-hour run.
- `make cost`: remaining budget per purpose, read from OpenRouter, plus the local ledger.

## 3. Test doubles: what is allowed

| Double | Allowed in | Rule |
|---|---|---|
| In-process port stubs (for example a `VCS` stub) | L1 and L2 of the **application** layer only | small, hand-written, never reused as an adapter |
| Recorded real payloads | L3 | scrubbed by the redactor and `gitleaks`; re-recorded when drift is found |
| Scripted agent over the real Switch and MCP protocols | L4 | deterministic; the protocol and transport are real, only the "brain" is canned |
| Mock LLM | never as a pass criterion | a recorded LLM response may speed a regression run (L2), but the live smoke (L5) is what proves an LLM-facing task |

The spec's earlier "fake GitHub, Switch and Graphiti with `httptest`" integration level is replaced by this table (D112).

**Test-only code never ships (D121).** Everything in this section (the scripted agent, doubles, the recorder, fixtures) lives in `internal/testkit/`, `cmd/heimdall-testagent/`, `testdata/` and `_test.go` files. Production packages never import it, release builds and container images exclude it, and production code has no test-mode flags, environment switches or special branches. CI runs `go list -deps` on every production binary and fails if `testkit` appears.

## 4. Language-model cost control

The key's limit is $4 and the testing cap is **$3**; the rest is held for the demo. Cost figures below are estimates from list prices (`deepseek/deepseek-v4-flash`: about $0.09 per million input tokens and $0.18 per million output) and **must be replaced with measured numbers after the first live run of each role**.

**Per-run estimates (worst case, several tool turns each re-sending context):**
- test-author about $0.03, one reviewer about $0.035, resolver about $0.012, profiler about $0.02, Graphiti backfill of a small corpus about $0.02, embeddings negligible.
- A full demo run (author, three reviewers, resolver, memory) is about $0.15 to $0.20, so a $0.60 rehearsal budget covers the 3-of-3 check and one spare.

**Testing budget ($3.00):**

| Use | Plan | Budget |
|---|---|---|
| Spikes (S1, S13, S14 and others) | about 6 small runs | $0.20 |
| L5 live smokes during development | 5 LLM-facing tasks, about 6 runs each | $1.00 |
| L6 evals | one full pass of the reduced corpora (about $0.9) plus partial reruns | $1.00 |
| L7 rehearsals | three D60 runs plus one spare | $0.60 |
| Reserve | | $0.20 |

**Controls** (T19 builds them):
1. **Separate keys with their own limits** (set in the OpenRouter dashboard by the account owner): `test` ($1.20, spikes and smokes), `eval` ($1.00), `rehearse` ($0.60), and the current key held back for the demo. OpenRouter enforces the cap, so a bug in our code cannot overspend. We currently have one key; creating these is a pending action for the owner.
2. **`make e2e`, `make eval` and `make rehearse` read the key's remaining balance and refuse to start** if it is below the estimated cost of the run.
3. **A cost ledger** (`testdata/cost-ledger.jsonl`, committed) records the purpose, task id, tokens and dollars of every live run, measured from the key's usage before and after.
4. **Outcome assertions, not sameness.** The model runs with its normal settings. Live tests assert outcomes (a planted issue found, with evidence the code verified; a valid submission) over a few runs, and never exact wording or an identical result every time (D122).
5. **Stop rule.** If a live run costs more than twice its estimate, stop, record it, and recalibrate before running anything else.
6. **One provider.** Everything runs on OpenRouter; the Hugging Face credits are not used unless the testing budget runs out, and then by changing the configured provider, not by an automatic chain (D133).
7. **Soak and chaos use scripted agents**, so they cost nothing.

## 5. The per-task loop

For every task in the plan:

0. **Board before and after.** Run `make prototype ONLY=<scenario>` for the step this task owns (`MVP_SCENARIOS.md` section 7) and keep the red line. The same line must turn green at step 7, or move to a later, named failure.
1. **Read** the task, its spec sections and the decisions it cites. Write or update the package's LLD note (`doc.go`): purpose, API, invariants, failure modes, test list.
2. **Red.** Write the tests from the "Done when" column and the matrix below. Run them. Confirm each fails for the right reason. Keep the output.
3. **Green.** Implement the smallest change that passes. Keep `make fast` green throughout.
4. **Refactor** against `CODING_STANDARDS.md`.
5. **Verify.** `make verify`, then the real check for the task (`make e2e T=<id>`, with L5 when LLM-facing).
6. **Adversarial pass.** Try to break it: invalid input, replay, two things at once, a dependency down, wrong person, stale SHA, oversized input. Each break that matters becomes a test. At gates, a separate reviewer (a fresh session or subagent) does this pass, because the author is the worst person to find their own gaps.
7. **Record.** In the tracker note: the commands run, exit status, key output lines, live cost, and anything **not** verified. Update `docs/PROGRESS.md` (state, next task, how to resume, failing tests, spend, open questions).
8. **Commit** as `T##: <what>`. The pre-commit hook runs `make fast`.

**Definition of done (all must hold):** the owned board step turned green (or is `blocked` with a named prerequisite and written "not verified"), red test seen failing, green after, `make verify` passes, the real check passed with evidence recorded, nothing marked skipped, spend within budget, standards met, progress file updated.

## 6. Test data and environments

- **Real recordings** live in `testdata/real/<system>/` with a README on how each was captured. Secrets and personal data are scrubbed; `gitleaks` runs on the folder.
- **Hermetic fixture repos** live in `testdata/repos/`: a Go service (the old `pay-demo`), a Python service with a Postgres service (shaped like Jurist AI's backend), a Node app, and a small JVM project. Each has known red and green tests and a known profile, so the generic sandbox (D98) is tested on many shapes without touching real repos.
- **Live demo target:** Jurist AI. Its scenario (ticket, planted issues, the frontend client as the cross-team fact) is authored as task T75. PRs target the long-lived `heimdall-demo` branch, never `main` (D154).
- **Live test hygiene:** Jira issues created by tests carry the label `heimdall-test` and are cleaned up; the simulator keeps a run manifest of what it made, plus branches named for its tickets (`<KEY>-*`, made by the product), and deletes those; test PR branches are `heimdall-test/<run-id>`; Mattermost channels are prefixed and archived. Tests never touch anything outside the manifest (D154).
- **Local world secrets:** the server on a laptop uses a `heimdall.toml` with `secrets_dir = "secrets"`, filled by `make secrets-local` (D148). This is configuration, so production has no test branch. The simulator reads `.env` for its own credentials and hands them out per call.
- **Where things run:** L0 to L3 run on the laptop and in CI. L4 runs from the laptop against real GitHub and Jira, and against the VM's Switch through the port-forward in `RUNBOOK.md`, until T105. L5 to L7 and the runner's isolation checks run on the VM.
- **Flakes:** no retry-until-green. A flaky deterministic test is a bug and is fixed or deleted with a note. Live LLM tests are judged on outcome sets and may be run up to twice by the rule in section 4 only if the first run was an infrastructure failure, not a wrong answer.

## 7. Verification matrix by feature

"Red" is the first failing test. "Real" is the real-system check. LLM cost is the live-run estimate (see section 4).

### G0: spikes (throwaway code, but the findings are recorded and become fixtures)

| Task | Red | Real | LLM |
|---|---|---|---|
| T01 S1/S11 Switch room, agent reply, command path, per-room session | each check a to g in `STAGE_1_FIXES.md` | real Switch and Mattermost | about $0.01 |
| T02 S12 human identity by sender id | same `sender` across posts, distinct between people, agent's `heimdall uphold` post maps to no person | real room, two people | $0 |
| T03 S14 OpenCode lockdown | attack check in CODING_STANDARDS 15; MCP call with the agent's own token works; `auto_mode` confirmed | real VM | about $0.01 |
| T04 S4 required check blocks merge | merge button blocked | real GitHub App and branch protection | $0 |
| T05 S6 missing route is `<failure>` | run under pytest and `go test`; XML has `<failure>` not `<error>` | real runners in the sandbox | $0 |
| T06 S13 Graphiti, hosted embeddings, group filter | seeded fact for group A absent for group B | real Graphiti, FalkorDB, OpenRouter | about $0.02 |
| T09 S15 Teams round trip | message arrives and `heimdall …` parses | real Teams | $0 |
| T95 tunnel | teammates log in from another network | real tunnel | $0 |
| T91 S18, T93 S20, T94 S21, T96 S22 | each spike's pass condition written as a failing check first | real AWS VM, Mattermost, Teams, KAN | $0 |

### G1: skeleton

| Task | Red | Real | LLM |
|---|---|---|---|
| T00 toolchain, T18 harness | `make tools` versions print; a deliberately failing test blocks the commit | real machine, CI | $0 |
| T10 repo, CI, dependency rule | a deliberately bad import must fail CI | CI run on GitHub | $0 |
| T11 frozen interfaces | schema round trip and golden JSON fail before the types exist | n/a | $0 |
| T12 compose skeleton | `doctor` reports unhealthy services | real Docker | $0 |
| T13 registry | a stub adapter must be selectable by name | n/a | $0 |
| T14 `connect` CLI | golden CLI output, `--json` shape | real read-only `Check()` on GitHub and Jira | $0 |
| T15 runner | escape attempts (socket, metadata, network, write outside `/out`) must fail | real runner, real Docker | $0 |
| T16 standards, T17 config | each lint rule and each bad config must fail the build or the boot | CI | $0 |

### G2: walking skeleton

| Task | Red | Real | LLM |
|---|---|---|---|
| T20 event store | version conflict, update and delete refused, hash chain breaks on tamper, `kill -9` mid-append loses nothing | real Postgres | $0 |
| T21 reducer | state by event table (every pair has a test; a missing pair fails the build), replay property | n/a | $0 |
| T22 River and outbox | crash between commit and effect gives exactly one effect; duplicate key ignored | real Postgres, real GitHub check | $0 |
| T23 step engine | an invalid workflow fails closed | n/a | $0 |
| T24 GitHub adapter | rate-limit backoff, token narrowing (a narrowed token cannot write contents) | real App on the test repo | $0 |
| T25 and T36 tracker, Jira poll | ADF golden from real KAN tickets, fuzzed walker, overlap window finds a late-indexed move, no double trigger | real KAN | $0 |
| T26 Switch client | stream gap and reconnect recover missed messages | real Switch with the scripted agent | $0 |
| T27 agent runtime | controller enrolled; one agent per Linux user; ack deadline, then `unavailable`; egress blocked; agents back after `sudo reboot` | real VM | $0 |
| T97 Switch deploy | `sudo reboot` and the stack returns with rooms intact | real VM | $0 |
| T28 MCP server | schema validation, role and token scoping (wrong role refused), oversized input refused | real MCP client | $0 |
| T29 sandbox | import-error test rejected, assertion-red accepted, flaky quarantined (on each fixture repo) | real runner | $0 |
| T34 repo profile | validator rejects an off-allowlist image, network use, oversize limits; detectors right on every fixture repo | real runner; the profiler agent on Jurist AI | about $0.02 |
| T30 test-author flow | invalid submissions refused with structured errors; red-first, AC coverage | scripted author at L4, then a live author on a fixture ticket at L5 | about $0.03 |
| T31 and T75 seeds | fixture red/green expectations; `make fixtures-push` publishes a real repo; Jurist AI scenario defined first | real GitHub and Jira | $0 |
| T34b Jurist AI profile | `--network none` plus a services network; pgvector migration applies; no model call in the chosen tests | real runner | about $0.02 |
| T32 commands, T37 chat decisions | grammar table and fuzz; wrong person, agent sender and stale SHA refused | real Mattermost message | $0 |
| T35 token source | each operation gets only its permission | real App | $0 |
| T38 scripted agent, T39 fixture repo matrix | n/a (they are test tools) | self-tested on real Switch | $0 |
| T105 `make deploy`, T16b `make sandbox` | run twice changes nothing and the service stays up; a sandboxed builder cannot read `~/.ssh` or `.env` | real VM; real container | $0 |
| T19 budget guard, T98 board | `make e2e` refuses on low balance; each quality check catches a deliberately wrong behaviour | real key; real board | $0 |

### G3: review loop

| Task | Red | Real | LLM |
|---|---|---|---|
| T40, T41 reviewer and resolver skills | schema-valid output on three sample diffs and cases | live agent smoke | about $0.035 and $0.012 per run |
| T42 context bundles | deterministic hash, size caps, independence (no other reviewer's findings) | n/a | $0 |
| T43 reviewer pick, T44 `submit_review` | selection table; invalid submissions refused with errors the agent can act on | real room | $0 |
| T45 evidence verification | a fake repro never reaches E1; a real one does; a push re-runs E1 and E2 by itself (D152) | real runner | $0 |
| T46 slop rule | table of PR shapes, including a PR with no ticket (D146); measured p95 under 15 s | real PR events | $0 |
| T47 merge rule and check | each clause has a flip test, including another ticket's tests merged into the base (no false `tests_changed`, D150); a real PR goes red, fixed, approved on head, green | real GitHub | $0 |
| T48 pack builder | planted secrets and canaries never reach the pack; fuzzed redactor | local | $0 |
| T49 memory | a group A fact never answers a group B query | real Graphiti | about $0.02 |
| T50 G3 demo | the scripted-agent run first, then one live run | full real stack | about $0.15 |

### G4: product, quality, freeze

| Task | Red | Real | LLM |
|---|---|---|---|
| T60 challenge rounds, T61 test sign-off | `heimdall challenge` from chat; a challenge without new evidence cannot move a finding; sensitive ticket waits for approval | real room | scripted, then about $0.05 |
| T62 security | server-side invariants (output filter, untrusted wrapping, canaries) on every push; the 12 injection payloads against a live agent are an L6 eval | live agents | about $0.42 per full run |
| T63 dashboard | HTTP and SSE tests; empty and error states | real run | $0 |
| T64 boot reconciliation | stop the server mid-run, start it, assert no lost events and no false green | real stack | $0 |
| T65 chat memory, T66 baselines | observed chat facts cannot block; baselines saved verbatim with date | real | about $0.01 and $0.02 |
| T67 Teams | `heimdall …` and thread replies work | real Teams | $0 |
| T68 local mode | a clean container with its own Docker daemon reaches red-then-green with real agents, no Jira and no public URL (D157) | clean container | about $0.15 |
| T69 planted-issue check | each planted issue found with verified evidence in at least 2 of 3 runs | full real stack | about $0.55 |
| T71 reliability kit, T74 chaos-lite | `make recover` from a stopped VM; 2-hour soak passes; section 8 ends with no lost events and no false green | real VM | $0 |
| T99 guided setup | stop partway and resume; a failed check names the fix; integrations with no real run show `untested` | real systems | $0 |
| T72 product experience | a new user connects three systems in under 15 minutes by following only the prompts | a real run by someone who did not write it | $0 |
| T73 agent quality | each planted issue found in at least 2 of 3 runs, baseline comparison saved | evals | within the eval budget |

## 8. Chaos-lite and soak (D117)

The k6 load test stays out of scope. A small scripted set runs on the real stack (scripted agents, no LLM cost): kill the server mid-step, kill the runner, stop Postgres for 30 seconds, pause the Switch container, and flood the sandbox queue. Each must end with no lost events, no false green (red or waiting, never green), and recovery without manual steps. The 2-hour soak opens a PR every 5 minutes and checks memory, queue depth and gate latency.

## 9. Rules for the builder (these go into the repo's `CLAUDE.md`, T10)

- Follow the loop in section 5 for every task. Never mark a task done without the evidence line.
- Write the failing test first, and read why it fails.
- Before any live-LLM run: run `make cost`, state the expected cost, and stay inside the budget table. Record the real cost afterwards.
- Never use a fake as a product adapter. Never weaken or delete a test to get green; if a test is wrong, say why in the commit and fix it.
- If something could not be verified (no VM yet, a spike failed), write "not verified" in the tracker. Do not guess.
- At each gate: run the full layer set, then request the adversarial review by a fresh session or subagent, and fix what it finds before declaring the gate passed.
- Keep `docs/PROGRESS.md` current so any new session can resume from it alone.
- Test scaffolding stays in test code and never reaches a production binary (D121).
- Do not add a fallback, a second path or an escape hatch unless a real, observed failure needs it. A spike picks one option; delete the other (D122).
- Do not constrain the model beyond what safety and correctness need: no forced temperature, no exact-output checks, no fixed attempt counts. Judge LLM behaviour by outcomes over a few runs (D122).

## 10. What this strategy deliberately does not do

- It does not run full agent evals on every push (they cost money and are slow).
- It does not chase the section 6.7 ship bars during the hackathon; those apply afterwards (D60).
- It does not test the setup skill on other people's laptops with Claude Code or Codex (OpenCode on the VM is the tested runtime, D72).
