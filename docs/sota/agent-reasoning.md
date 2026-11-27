# Agent reasoning (SOTA Phase 0, Nov 22, 2026)

Scope: the reasoning quality of every agent role (test author, reviewer, resolver, profiler, lead): the inputs they get,
the tools they choose, what they submit, the noise they avoid, and the cases they handle. Sources: the skills
(`skills/*.md`), `internal/domain/workflow/kinds.go`, `get_task` (`internal/transport/mcp/get_task.go`), the live runs
H11 (`docs/archive/hackathon/LIVE_RUN_H11.md` D4 to D6) and H13 (PROGRESS: D178, D180, PR #18), and the event log on the VM (all live runs,
Nov 7 to Nov 21). All agents run `deploy/deepseek-ai/deepseek-v4.1-flash-1017`.

## 1. Desired state

- **Test author.** Reads the criteria, writes the smallest tests that fail on the base for the right reason, proves it
  red first, and never writes a test that already passes on the base. Resubmits once with the rejection's exact cause.
- **Reviewer.** Stays in its lenses, starts from the leads and cards, proves each claim at the strongest tier it can,
  files each defect once, and says nothing on clean code. Finishes in a bounded number of calls and tokens.
- **Resolver.** Settles a challenged finding from cited evidence only, quotes it, and escalates when the evidence does
  not decide.
- **Profiler.** Reads the build and test setup, proposes a profile that passes its trial the first time, and on a
  failed trial fixes exactly what the log shows.
- **Lead.** Answers a person's question about a change from its recorded facts, and proposes a command instead of
  acting.
- **All roles.** Every agent ends its task with one submission, never loops on tools that fail, treats repository and
  log text as data, and runs within a known budget.

## 2. What the runs show

| Role | Observed | Evidence |
|---|---|---|
| Test author | 25 submissions, 12 verified red first, 6 rejected (Nov 7 to 19). Correct red-first rejections; one case could never pass red first (a boundary that already held on base, D4); after a submission it looped on room posts and never read its next task (D5) | event counts on the VM; `docs/archive/hackathon/LIVE_RUN_H11.md` D4, D5 |
| Reviewer | Recall 8 of 8 on PR #18, no false findings; about 33 tool calls and about 1.4M context tokens per review; filed every issue again on each head; ignored lens boundaries; one review ran out of output tokens on reasoning (D6, fixed by raising the limit to 32,000) | `lenses.md`; `performance.md`; `docs/archive/hackathon/LIVE_RUN_H11.md` D6 |
| Resolver | **Never run live**: no `case.opened`, `challenge.filed` or `verdict.submitted` exists in any change | VM event counts, Nov 22 |
| Profiler | First proposal refused for an unknown key, then fixed from the refusal (D178); wrote `go test` without `-json` because the skill's example did (D180), then fixed it from `previous_trial` in 17 s; the requested revision (golangci-lint with SARIF) was right on the first try in 21 s | PROGRESS H13 (D178, D180, revision run) |
| Lead | **Does not exist** (see `agent-triggering.md` T1) | `provisionAgents` |
| All | 7 `task.failed` events in all live runs; stalls recovered by the stall wake (PR #18 reviewer-2) | VM event count; PR #18 events 673 to 683 |

## 3. Gaps

| # | Gap | Severity | Evidence | Smallest correct fix | Test that would have caught it |
|---|---|---|---|---|---|
| R1 | **The resolver has never reasoned on a live case.** Its skill, tools and verdict rules are untested against a real challenge. | high | no deliberation events on the VM | Phase 2: a challenge on a true finding and one on a false finding; score the verdicts | Phase 2 suite |
| R2 | **Reviewers re-derive the whole review on each head.** They get the full brief again and re-file what they found (see `lenses.md` L1); the reasoning is not incremental. | high | PR #18 three heads; `performance.md` P1 | `get_task` on a later head gives the open findings with their re-check status and the diff since the last reviewed head; the skill asks only for new problems and for each open finding: still there or fixed | Phase 2: a second head that fixes one finding: the reviewer marks it fixed and files nothing else |
| R3 | **Fixed Nov 22.** `TestProfileSkillExamplesParseCleanly` parses every YAML example in `skills/profile.md` through `profileyaml.ParseApprovedProfile` and asserts `-json` acceptance output. | done | `internal/adapters/profileyaml/skill_example_test.go` | — | `TestProfileSkillExamplesParseCleanly` |
| R4 | **No reasoning budget per task.** Reasoning tokens counted against output and once took all 8,000 (D6); with 32,000 nothing bounds a long think, and the stall rule cannot tell thinking from stuck (`agent-triggering.md` T4). | medium | `docs/archive/hackathon/LIVE_RUN_H11.md` D6 | Per-kind budgets in `kinds.go` (calls, output tokens, wall time) shown in `get_task`, and liveness reading the agent's activity | Liveness and get_task tests |
| R5 | **Fixed Nov 22.** `mcp.Server.recordCall` now detects 3 consecutive identical failing tool calls (`repeatedFailures`) and converts the third into a guidance refusal telling the agent to stop retrying and move on. | done | `internal/transport/mcp/server.go`, `repeat_failure_test.go` | — | `TestRepeatedFailuresCountsConsecutiveMatchingFailingCalls` |
| R6 | **Fixed Nov 22.** `dashboard.ScorecardFromEvents` and `Service.Scorecard` compute per-role and per-seat quality metrics (filed, verified, upheld, dismissed, red-first rate, resolver cases/verdicts, profiler proposals/requests), served on `GET /admin/ops`. | done | `internal/app/dashboard/scorecard.go`, `cmd/heimdall-server/operator_ops.go` | — | `TestScorecardFromEventsCountsEveryRole` |
| R7 | **One model for every role.** The cheapest model writes tests, reviews money-moving code and would settle disputes; no role was compared with a stronger model. | medium | `agent_runtime.model` single value | Allow a model per kind in `heimdall.toml`; run Phase 2 with a stronger model on the resolver and one reviewer seat and compare scores and cost | Phase 2 comparison run |

## 4. Order of work

1. R6 (scorecard): every other gap needs numbers.
2. R1 and R2: the untested role and the costliest reasoning flaw.
3. R3 (skill example test) — cheap and stops a known class.
4. R4, R5, R7.
