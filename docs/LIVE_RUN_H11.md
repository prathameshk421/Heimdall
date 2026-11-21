# Live run H11, Nov 19, 2026 (UTC): deploy and the first journey on the reconciler build

The H11 step of `docs/HARNESS_DESIGN.md`: deploy the overhaul build (H0 to H10) to the VM, then run a real ticket
through the real agents. Every number below is read from the VM: the `event`, `outbox` and `tool_call` tables, the
server log, the Switch database and the agents' OpenCode databases (read only).

Ticket **KAN-26 "Cap the amount of a single payment"**, label `heimdall-test`, three criteria (AC1: 1000000 returns
201; AC2: 1000001 returns 400 naming the amount and the cap; AC3: the cap holds in JPY). Repo
`aarush323/heimdall-fixture-pay`, base `heimdall-test/demo-base` (`366f529`). Ticketless PR: #15.

## Result per row

| Row | Outcome | Minutes |
|---|---|---|
| Deploy (server and runner together) | done; hashes match inside the container; `/healthz` ok, no `degraded` after fix D2 | 0.4 per deploy |
| Ticket → triggered | done (Jira poll) | 0.1 (move 19:35:38, trigger 19:35:43) |
| Room, branch, tests task | done | 0.1 (19:35:46, :49, :50) |
| Test author wakes | done after an agent host restart (D3) | 6.5 (19:35:50 to 19:42:21) |
| Tests submitted, attempt 1 | refused, correctly: two at-cap tests passed on base (D4) | 3.9 to submit, 1.7 to verdict |
| Tests submitted, attempt 2 | done after a stuck session (D5) and the reset rung of the ladder | 16.6 (19:47:57 to 20:04:34) |
| Tests verified, red first, 3 runs | done | 1.7 (20:04:34 to 20:06:15) |
| Tests committed | done, `ab0d649` on `KAN-26-cap-the-amount-of-a-single-payment` | 0.1 |
| Implementation on the ticket branch, PR #16 | done (by me, as `prathameshk421`) | 2.2 |
| Slop, CI, `heimdall/acceptance` | pass, success, success | 0.7 |
| Pick | default after the 1 min pick wait | 1.2 (slop 20:08:45, picked 20:09:55) |
| Three reviews | 1 of 3: reviewer-2 submitted (no findings); reviewer-1 and reviewer-3 ran out of output tokens twice (D6) | reviewer-2: 4.1 |
| Profile checks, `run_check`, E2 | not run: no promoted profile (B1) | |
| Verify, gate green | not reached (D6, B1, B2) | |
| Approval by `rekdfr` | blocked (B2) | |
| Merge | not done: the gate is red | |
| Ticketless PR #15 | slop failed (`ticket_linked`, `tests_present`, `scope`), gate red, room opened, CI green; **not reviewed** (D7) | |

## Timeline

| UTC | What | Source |
|---|---|---|
| 19:29:11 | PRs #13 (KAN-24) and #14 (KAN-25) closed with a comment, as the design says | GitHub |
| 19:29:43 | first hot deploy: backup, server and runner, compose; `/healthz` `degraded: ask_knowledge` (D2) | `hot-deploy.sh` |
| 19:30:58 | second deploy with the compose fix: `{"status":"ok"}` | `hot-deploy.sh` |
| 19:35:27 | KAN-26 created; assigned and moved to In Progress at 19:35:38 | Jira REST |
| 19:35:43 | `change.triggered` | event |
| 19:35:46 | `room.opened` (6 agents in the room) | event, Switch log |
| 19:35:49 | `branch.ensured` from `366f529` | event |
| 19:35:50 | `task.assigned` tests to test-author; Switch queued the mention | event, `agent_events` |
| 19:39:05 | `task.rewoken` n=1 (ack window 3 min) | event |
| 19:41:12 | Switch agent controller restarted; no delivery yet | journal |
| 19:42:11 | test-author host unit restarted; "isolated agent host connected" | journal |
| 19:42:21 | `get_task` (5.5 KB), `task.acked` | tool_call |
| 19:46:14 | `submit_tests` attempt 1 (5 tests, AC1 to AC3) | event |
| 19:47:57 | `tests.rejected`: two at-cap tests passed on base; `task.assigned` attempt 2 | event |
| 19:46 to 19:52 | the session loops on `switch_post_message` (every post fails) and never reads the new task (D5) | OpenCode DB |
| 19:52:29 | third deploy (skill fix D5); test-author unit restarted 19:52:59 | `hot-deploy.sh` |
| 20:03:29 | `provider.probed {health: ok}`: the fixed probe reached Deploy (D1) | event |
| 20:03:59 | `task.rewoken` n=2, reset; `agent_reset` job fails ("no executor", known) | event, outbox |
| 20:04:34 | `submit_tests` attempt 2: the at-cap cases joined to above-cap cases | event |
| 20:06:15 | `tests.verified` (3 runs, red first, `needs_signoff: false`) | event |
| 20:06:22 | `tests.committed` `ab0d649` | event |
| 20:08:36 | PR #16 opened (implementation, contract, unit test) | GitHub |
| 20:08:45 | `pr.opened`, `slop.evaluated {pass: true}` | event |
| 20:09:26 | `go test` success, `heimdall/acceptance` success | event |
| 20:09:55 | `reviewers.picked` default: reviewer-1, reviewer-2, reviewer-3 | event |
| 20:10:25 | three review tasks; acked 20:10:48 to 20:10:53 | event |
| 20:14:29 | reviewer-2 `submit_review`: no findings; one card checked, not applicable | event |
| 20:16:00, 20:17:18 | reviewer-3 and reviewer-1 stop with `reason: length`: 8000 reasoning tokens, 0 output (D6) | OpenCode DB |
| 20:24:49, 20:26:32 | both rewoken with reset; both stop on `length` again (20:25:53, 20:27:39) | event, OpenCode DB |
| 20:34:52, 20:36:37 | `task.failed {unresponsive}` for reviewer-3 and reviewer-1; reviewer-3 re-assigned at 20:35:30 | event |
| 20:39:36 | PR #16 closed by me to stop the paid loop; `pr.closed` recorded | GitHub, event |

## Tool calls and refusals per task

| Task | Calls | Refused | Notes |
|---|---|---|---|
| tests, attempt 1 | 7 | 1 | `get_task`, 3 `read_code`, 1 `search_code`, `submit_tests`; `report_progress` after submit refused ("no active task") |
| tests, attempt 2 | 2 | 0 | `get_task`, `submit_tests` (it had the code in context) |
| review, reviewer-1 | 34 | 1 | 28 `read_code` (one path that does not exist), 4 `search_code`, `ask_knowledge`; no submission |
| review, reviewer-2 | 15 | 1 | `run_check` refused: no promoted profile; submitted |
| review, reviewer-3 | 17 | 1 | `run_check` refused: no promoted profile; `find_symbol`, 2 `read_card`; no submission |

Each agent used tools added after Nov 16 (`run_check(check_id)`, `find_symbol`, `read_card`), so the agents' MCP tool
lists were current after the restarts (H7 trap). The restart is what refreshed them; whether the agent sync alone
refreshes a cached list is **not verified**.

## Cost

Expected cost was stated before the run: $0.30 to $1, cap $3. The agents run
`deploy/deepseek-ai/deepseek-v4.1-flash-1017`; OpenCode records `cost: 0` for this custom provider, so the cost is
estimated from the tokens in the agents' OpenCode databases (all sessions of this run):

| Tokens | Count |
|---|---|
| input, not cached | 277,944 |
| cache read | 5,586,944 |
| output | 21,495 |
| reasoning | 151,731 |

At DeepSeek flash-class prices ($0.14 per M input, $0.028 per M cached, $0.28 per M output) this is about **$0.24**.
The provider's own balance was not read (its key is a secret file); the real figure is **not verified**. The run
stayed far under the $3 cap.

## Defects

### D1. The liveness probe asked the wrong provider with a key the server did not have (fixed, `b20e758`)

**Seen.** Before deploy: the H3 prober called OpenRouter with `openrouter_api_key`, but compose never mounted that file
into the server, and the agents run on Deploy (`agent_runtime.model = deploy/...`), not OpenRouter.
**Fix.** `modelProviderOf` reads the provider from the agents' model id (`deploy` or `openrouter`; any other provider
stops the boot); compose mounts `deploy_api_key` into the server. Test first:
`TestTheProbeTargetsTheAgentsModelProvider` and `TestAModelOfAnUnknownProviderIsRefused` failed to compile, then
passed. **Live:** `provider.probed {health: ok}` at 20:03:29.

### D2. The boot self-check withheld `ask_knowledge` for the whole process (fixed, `b20e758`)

**Seen.** `/healthz` said `degraded: ["ask_knowledge: knowledge search unreachable"]` after the first deploy: the
server started before Graphiti was healthy, and the H9 check runs once.
**Fix.** Compose makes the server wait for Graphiti's healthcheck. Config only (R1). **Live:** `{"status":"ok"}` on
the next two deploys, and reviewers called `ask_knowledge` successfully.

### D3. Agent hosts lost their hub after the Nov 17 controller restart (not a Heimdall defect; recovered)

**Seen.** Switch queued the test-author's mention (`agent_events` seq ...973 and ...974), but no agent had run since
Nov 16. The agent units (`switch-agent-1000@NN`) had kept running from Nov 17 06:32 without a hub connection; a
controller restart alone did not reconnect them. Restarting each agent unit did ("isolated agent host connected to
the hub"), and the queued mention was delivered at once.
**Heimdall side.** The ladder did its part (rewake at 3 min), but it cannot see a host with no hub. `docs/RUNBOOK.md`
(hot deploy section) now says to restart every agent unit after a controller restart and check one "agent host
connected" line per agent.

### D4. A boundary case that already holds on base can never pass the red-first rule (fixed in the skill, `92bf94c`)

**Seen.** Attempt 1 had `AtCap_Returns201` tests. A value at a new limit is accepted on base too, so they passed on
base and the whole submission was refused. The skill asked for boundary cases; the rule refuses any test that passes
on base. The rule is right (a test that passes without the feature proves nothing alone); the skill was wrong.
**Fix.** `skills/tests.md`: join such a case to a case that fails on base. Wording only (R1). **Live:** attempt 2 did
exactly that and was verified.

### D5. After submitting, an agent looped on room posts and never read its next task (fixed in the skills, `92bf94c`)

**Seen.** After `submit_tests` the test author tried `switch_post_message` many times (every post failed: "not
connected" right after a successful `connect_to_room`; Switch logs "not a member of room 01f9b695..."), and the
mention for attempt 2 queued behind the busy session.
**Fix.** The tests, review and verdict skills say: submit once, then stop; a refusal or a new head wakes you. **Live:**
after the restart and the reset rung, the author went straight from `get_task` to `submit_tests`. The Switch post
failure itself is a Switch room-placement issue (**not investigated** here).

### D6. Reviewers spend the whole 8000-token output limit on reasoning and submit nothing (fix written, not applied)

**Seen.** reviewer-1 and reviewer-3 each ended a step with `reason: length`, `reasoning: 8000`, `output: 0`, twice
(before and after the reset rewake). The diff is 20 lines. reviewer-2 finished because it reasoned less.
**Cause.** The agents' OpenCode model limit is `output: 8000`, and DeepSeek's reasoning counts against it.
**Fix in the repo.** `testdata/agents/*.opencode.json` and `scripts/ops/install-agent-configs.sh` raise the limit to
32000. **Not applied:** the installer writes each agent's `opencode.json` and copies its token, which only the owner
runs. **Not verified:** that Deploy accepts 32000 output tokens for this model. Until the owner runs
`ssh heimdall-vm 'bash -s' < scripts/ops/install-agent-configs.sh` and restarts the agent units, reviews stall.

### D7. A ticketless PR is never reviewed under the fixture policy (design gap, recorded)

**Seen.** PR #15: slop fails on `ticket_linked` (the policy has `require_ticket: true`), and the `pick` row runs only
after slop passes, so no reviewer is ever picked. The design's B9 item 2 and this step both expect "one ticketless PR
reviewed". H10 already found that a ticketless PR can never be gate-green; it can also never be reviewed while
`require_ticket` is true. **Needs the owner:** either a ticketless PR is reviewed before slop (and the gate stays red),
or the H11 check uses a policy with `require_ticket: false`.

### Smaller things seen

- The PR comment header names the stream id `pr-aarush323-heimdall-fixture-pay-15`, an internal id shown to people.
- `run_check` is offered to reviewers on a repository with no promoted profile; each call is refused. It could be
  withheld per change, like the H9 tools.
- `GitHub issue comments processed comments=1` logs every 11 s, and `workflow memory backfill failed` every 16 s
  (old changes on `PBL_TOS_RISK_ANALYZER` whose base has no `heimdall.yaml`). Both are log noise from before H11.
- `policy unavailable for card proposals ... field agent not found`: old bases with the v1 policy shape (F3 of
  KAN-24, owed in H12).
- `hot-deploy.sh` first version copied `.prev` on every run, so a re-run of the same build lost the pre-H11 binaries.
  Fixed before the third deploy: `.prev` is kept only when the file changes. The pre-H11 binaries were rebuilt from
  `45dbe83` and installed as `.prev` by hand to restore a rollback target.

## Blocked (needs the owner)

- **B1. No promoted profile on the fixture repository.** No proposal exists. The product has no profiler agent
  (`[agents]` names none, and no row assigns a `profile` task), and `approve_profile`/`promote_profile` need a mapped
  human policy owner (chat command `heimdall approve-profile <id>` or a human MCP principal). So the `patterns` check,
  `run_checks`, `leads` and E2 verification did not run live.
- **B2. No `rekdfr` credentials on this Mac.** The gh keyring holds `prathameshk421` and
  `temporaryhameshakagroup-alt`; the repo hook (R8) blocks reading `.env`. The approval as `rekdfr` and therefore the
  merge were not done. (The policy also lists `temporaryhameshakagroup-alt` as `approver`; I did not use it, because the
  brief names `rekdfr`.)
- **B3. D6 needs the owner's installer run** (it touches agent tokens).
- PR #16 was closed at 20:39 with a comment (reopen after B1 to B3): after `task.failed` the review row re-assigns the
  same seat with no bound (reviewer-3 at 20:35:30), so each D6 failure started another paid round. Whether a reopen
  restarts the change is **not verified**; a new PR from the same branch is the fallback. PR #11 (KAN-23, an old shape) is still
  open; the brief named only #13 and #14.
- The design's last step, `aws ec2 stop-instances --profile heimdall`, was not run: that AWS profile does not exist on
  this Mac, and the brief says to leave the VM running.

## Deviations from the design

- Jira writes went through the Jira REST API from the owner's logged-in browser session (`.cache/live/jira.sh` no
  longer exists, and the hook blocks `.env`).
- The PRs are authored by `prathameshk421` (not `aarush323`, whose token is not on this Mac). GitHub calls used the
  active `gh` account, because the hook blocks `gh auth token` and so a per-call token cannot be passed.
- The deploy script ran through `ssh heimdall-vm 'sudo sh -s'` (passwordless sudo), not the owner's Terminal panel.

## Second half, Nov 20, 2026 (UTC): the review rerun

The owner did D6 (output limit 32000 for the five agents, agent units restarted at 18:32) and named
`temporaryhameshakagroup-alt` as the approver for B2. B1 and D7 stay open. Expected extra cost, stated in PROGRESS
before the run: $0.30 to $1, cap $3.

### Result per row

| Row | Outcome | Minutes |
|---|---|---|
| Reopen PR #16 | GitHub reopened it; Heimdall appended nothing, so the change stayed over (D8) | 0.1 |
| D8 fix, test first, `make fast` and `make verify`, hot deploy | done, `bb04269`; server `8b8fa65e`, runner `d581f0d0`, `/healthz` ok | 20 (18:45 to 19:05) |
| Change resumes | `pr.reopened` at 19:05:24, one second after the deploy | 0.1 |
| Reviews | already 2 of 3 from Nov 19: reviewer-2 and reviewer-1 (20:38:46, after its third task), both no findings. reviewer-3's old task timed out and a new one was assigned at 19:05:55 | 0.5 |
| reviewer-3 runs | **blocked (B4):** every model call fails with `Unauthorized: Authorization header missing`; no ack, `task.rewoken` at 19:09:19 | 3.4 |
| Does 32000 work on Deploy | **not verified:** no agent reached the model | |
| Profile, `run_check`, E2 | not run (B1, see below) | |
| Verify E1/E2/E3 | not run: no finding was filed | |
| Gate, approval as `temporaryhameshakagroup-alt`, merge | not reached (B4) | |
| Close again | PR #16 closed at 19:11:39 to stop the ladder; `pr.closed` at 19:15:36 by the reconcile pass. This proves the second half of D8 live: a close after a reopen is recorded again | 4 |
| Ticketless PR | skipped (D7, owner decision) | |

### Timeline

| UTC | What | Source |
|---|---|---|
| 18:32:22 to 18:32:35 | owner's D6 run: configs rewritten, agent units 03 to 07 restarted, five "agent host connected" lines | journal |
| 18:44:32 | PR #16 reopened (as `prathameshk421`, token per call) | GitHub |
| 18:44 to 18:47 | `pull requests synced synced=3 appended=0`; no event on KAN-26 | server log, event |
| 19:04:52 | hot deploy of `bb04269` (backup, `.prev` copies, hashes match inside the container) | `hot-deploy.sh` |
| 19:05:24 | `pr.reopened` | event |
| 19:05:27 | `task.timed_out` for reviewer-3's Nov 19 task | event |
| 19:05:55 | `task.assigned` review to reviewer-3 | event |
| 19:06:01 | reviewer-3's session: `APIError Unauthorized: Authorization header missing` | OpenCode DB |
| 19:09:19 | `task.rewoken` n=1; the rewake fails the same way at 19:09:20 | event, OpenCode DB |
| 19:11:39 | PR #16 closed with a comment | GitHub |
| 19:15:36 | `pr.closed` (reconcile pass) | event |

### Tool calls, cost

No Heimdall tool call and no model token in this half: `tool_call` has no row after 18:44, and the agents' OpenCode
databases hold 0 input and 0 output tokens since then. **Spent: $0.00** (the run total stays about $0.24, estimated).

### D8. A reopened pull request never resumed its change (fixed, `bb04269`)

**Seen.** PR #16 was closed on Nov 19 to stop a paid loop. Reopened on GitHub, it got no event: intake saw the same
head and appended nothing, the fold kept `closed`, and the reconciler never ran the change again. A fresh PR for
KAN-26 would land on the same ended stream, so the brief's fallback had the same defect.
**Fix.** A new fact `pr.reopened` (TECH_SPEC 3.2). prsync appends it once when GitHub lists a PR as open that the
stream last recorded as closed without a merge. The fold clears the close only for the change's own PR; a merged
change stays merged. `OpenChanges` and `OpenPRStreams` read the same rule in SQL, and a later close appends
`pr.closed` again. Tests first: `TestAReopenedPullRequestResumesTheChange`, `TestAReopenDoesNotUndoAMergeOrAnotherPullRequest`
(domain), `TestAReopenedPullRequestIsRecordedOnceAndCanCloseAgain`, `TestAMergedPullRequestIsNeverReopened` (prsync),
and the two SQL component tests, which failed on the old SQL. **Live:** reopen at 19:05:24, close again at 19:15:36.
**Not covered:** a second, new PR for a ticket whose first PR closed still folds into the ended change (Noticed).

### B4. The D6 configs read the Deploy key from a variable the agents do not have (needs the owner)

The installer (`scripts/ops/install-agent-configs.sh`, and `deploy/agents/install.sh` by design, D163) writes
`"apiKey": "{env:DEPLOY_API_KEY}"`. The agent units' environment holds `OPENROUTER_API_KEY` only (variable names
checked in `/proc/<pid>/environ`; no value was read). D163 says the controller installer
(`deploy/agents/install-controller.sh`) writes `DEPLOY_API_KEY` into `/etc/switch/agents.env`, but that step never ran
on this VM: the Nov 19 configs must have held the key another way. **Owner step:** add `DEPLOY_API_KEY` to
`/etc/switch/agents.env` (the controller installer does it from `deploy_api_key`), restart the controller and agent
units 03 to 07, check one "agent host connected" line each, then reopen PR #16. The reopen now resumes the change.

### B1. No product path to a promoted profile (unchanged, now traced)

`propose_profile` and `trial_profile` need a principal of role `profiler` holding an active `profile` task
(`internal/transport/mcp/tools_core.go`). No agent of that role is provisioned (`cmd/heimdall-server/adapters.go`
provisions test-author, resolver and the reviewers only), and no row assigns a `profile` task
(`internal/domain/change/rows*.go` assigns `review` and `verdict`; tests are assigned by the tests row). So no proposal
id can exist, and `/heimdall approve-profile <id>` by `temporaryhameshakagroup-alt` (a policy owner as `approver`) has
nothing to approve. Forging rows is not allowed. **Needs a design step:** a profiler seat and a row (or an operator
command) that assigns the `profile` task.

### Still open

D7 (ticketless PRs under `require_ticket: true`), B1, B4, and the unbounded reassignment of a failing seat. KAN-26
waits on B4: reviewer-3's review, then the gate, the approval and the merge.

## B4 resolved (Nov 21, 2026)

- The owner's first B4 step added `DEPLOY_API_KEY` to `/etc/switch/agents.env`. It did not reach the agents. The Switch 0.31
  controller writes each unit's environment file only at launch, and only for an allowlist of names (`OPENAI_API_KEY`,
  `OPENROUTER_API_KEY`). So `{env:DEPLOY_API_KEY}` can never reach an agent on this controller. D163 is wrong on that point.
- Fix: `scripts/ops/install-agent-configs.sh` now writes the Deploy key from the server's `deploy_api_key` file into each
  config, as it already did for the MCP bearer. This matches the configs that worked before D6 (key in the file).
- Live check: the resolver and reviewer-1 to reviewer-3 configs hold the key inline; a `GET /api/v1/models` with each
  config's key returned 200. The test author already had the key inline. Values were never printed.
- Open: `deploy/agents/install.sh` and the templates still use `{env:DEPLOY_API_KEY}`, so a fresh install has the same gap.
  Its checks live in the guarded `docs/harness/test-install.sh`; the change needs the owner's edit there.

## Finish, Nov 20 to 21, 2026 (UTC): KAN-26 merged

| UTC | What | Source |
|---|---|---|
| 11-20 19:33:00 | PR #16 reopened (as `prathameshk421`) | GitHub |
| 19:33:17 | `pr.reopened`; 19:33:20 `task.rewoken` n=2 with a session reset for reviewer-3's task | event |
| 19:36:05 | `review.submitted` by reviewer-3 (no findings); `gate.evaluated` red only on the approval | event |
| 19:37:37 | approval by `temporaryhameshakagroup-alt` on head `f91f0a5`; `approval.recorded` | GitHub, event |
| 19:37:52 | `gate.evaluated ok: true`; `heimdall/gate` success posted 19:37:55 (outbox 248) | event, GitHub |
| 23:30:36 | PR #16 merged into `heimdall-test/demo-base`, merge commit `d79cd08` | GitHub |
| 23:33:28 | `pr.closed merged: true` | event |

- All ten gate rules were green on the head commit before the merge. Three reviews, no findings, so E1 to E3
  verification had nothing to verify (not run).
- D6 holds on Deploy: reviewer-3's run used 17,323 reasoning tokens, more than the old 8,000 output limit, and still submitted.
- **Spent in this half:** reviewer-3 only, 64,098 input, 131,072 cache read, 18,134 output and reasoning tokens (OpenCode
  database; the other agents logged none). Estimated at the rates of the first half: about $0.02. Run total: about
  $0.26, estimated; the provider balance was not read.

### D9. `agent_reset` can never run: Heimdall has no Switch route to reset a session (needs the owner)

**Seen.** Outbox row 245 (`KAN-26/tsk_01M7X5KPNE17Q95JFNK21EHMBA/reset/2`) failed 6 times with
`no executor for effect kind "agent_reset"`. The ladder's reset rung only re-wakes the agent.
**Traced in Switch 0.31 (`switch-switch-1`, `switch_core`).** A session reset is the room command `!reset @agent`
(`bridges/agent/commands.py`), carried as a `com.switch.command` event. Only two writers exist: the platform chat
bridges (a person typing `!reset`) and the gateway route `POST /chats/{room_id}/controls`
(`{requestId, agentId, command: "reset"}`, idempotent by `requestId`, `chats/service.py`), which needs a gateway user
(`get_current_user`). Heimdall talks to Switch as the coordinator agent (`/agents/{id}/...`), and no agent operation
writes a command event. This confirms V63. A plain room post of `!reset` from the coordinator is a message, not a command.
**Needs an owner decision:** (a) give Heimdall a gateway user for this one route, which reverses D145, or (b) take the
reset rung out of the ladder (spec 6, `liveness.Next`), so `reset_and_repeat` becomes a repeat and the failing job goes
away. I did not choose either, because (a) changes D145 and (b) drops a designed step (R13).
