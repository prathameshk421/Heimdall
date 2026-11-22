# Agent triggering (SOTA Phase 0, Nov 22, 2026)

Scope: every message and event that should start, resume, wake, reassign, time out or stop an agent; every platform and
room; person and agent senders; duplicate, replayed, stale and out-of-order input. Sources: `AGENT_INTERACTION_ARCH.md`
(F1 to F6, 6.3), `TECH_SPEC.md` 6.2, `HARNESS_DESIGN.md` B3, the code at `7f47a05`, and the H11 and H13 live runs.

## 1. Desired state

- **One path in.** An agent starts work only because the server assigned it a task (`task.assigned` plus one wake
  line in the change's room); it learns everything from `get_task`. No agent acts on chatter it was not addressed in.
- **Every trigger is covered.** A ticket, a PR, a push, a reopen, a ticket edit, a challenge, a profile request and a
  person's question each wake exactly the agent that owns the next step, once. A ticket edit that changes the acceptance
  criteria re-opens the tests for the new criteria.
- **Quiet agents are handled without a person**, up to a bound: re-wake, check the provider, wake again after a stall,
  fail, reassign, then ask a person (D172, D173). Cost per stuck seat is bounded and visible.
- **People can talk to Heimdall in plain words.** `@heimdall why is KAN-22 blocked?` gets an answer built from the
  change's facts; `@security is F3 real?` reaches that seat in the change's thread; grammar commands run without a model.
- **Senders are told apart by mapping, not by trust.** A command counts only from a mapped person; an agent can never
  run a deciding command, even by quoting one; an unmapped person is told how to get mapped.
- **Replays change nothing.** A duplicate delivery, a replayed webhook, an edited message and an out-of-order event
  produce no second wake, no second task and no second command.
- **Every platform behaves the same**: Slack, Discord, Mattermost, Telegram and GitHub comments, including threads,
  edits and mentions; where a platform lacks a feature, the difference is written down.

## 2. What exists

| Trigger | Behaviour | Evidence |
|---|---|---|
| Task assignment | A row emits a Task action; the reconciler records `task.assigned` and a `task_assign` job that invites the agent and posts `@<agent> <ticket> is yours: <work>. Start with heimdall_get_task` | `internal/app/reconcile/tasks.go` `assign`; `internal/app/effects/task_assign.go:79` |
| Wake room | Always the change's room (`RoleRooms` exists in the executor but nothing sets it) | `task_assign.go:16-18,50`; no `RoleRooms` in `cmd/` (grep) |
| Ack | The agent's first tool call on its task records `task.acked`; there is no ack tool | `TECH_SPEC.md` 6.2 item 3 |
| Liveness | Ack window 3 min, stall 10 min, provider probe, one stall wake, then fail; 2 failures per input, then a person | `internal/domain/liveness/next.go`; `rows.go` `capTask`; live PR #18: reviewer-2 `task.rewoken{stall}` 21:50, ack 21:55, review 21:57 |
| New head | A live task on an old input is superseded and a fresh one assigned | `tasks.go` (`TaskSuperseded`); live PR #18 three heads |
| Reopen | `pr.reopened` resumes the change (D8) | `LIVE_RUN_H11.md` D8; `bb04269` |
| Profile | Profile row assigns `first`, `after-<id>` and `revise-<id>-<t>` tasks (D175, D184) | `rows_review.go` `profileNext`; live Nov 21 14:10, 15:07, 19:49 |
| Commands | Plain `heimdall <verb>` in chat, `/heimdall` on GitHub; parsed only for mapped people; agents' commands ignored | `internal/domain/commands/parse.go:32`; `internal/app/collaboration/commands.go:113-118` |
| Shared channel | A command must name its ticket unless the channel has one ticket or the verb is repository-scoped (D193) | `commands.go` `HandleSharedRoomMessage` |
| Replays | Commands carry an idempotent reply key per message; webhook deliveries are deduplicated by id; the Jira poll dedupes by (key, changelog id) | `commands.go` reply keys; `webhook_delivery` table; CLAUDE.md |

## 3. Gaps

| # | Gap | Severity | Evidence | Smallest correct fix | Test that would have caught it |
|---|---|---|---|---|---|
| T1 | **Partly fixed Nov 22.** A fixed status reply, not the lead agent: it ignores the question's words and always answers with the stage, gate reasons, open findings and active tasks, which covers "why is it blocked?" but not other questions. The model-backed lead of the design is still open. Mapped humans' `@heimdall ...` chat mentions are routed by `collaboration.Processor` (`Questioner`) to `commandRuntime.AnswerQuestion` (stage, PR, gate reasons, open findings, active tasks); unmapped senders and agents are ignored. Live chat verification not verified yet. | done | `internal/app/collaboration/commands.go`, `cmd/heimdall-server/command_question.go` | — | `TestMentionQuestionAnswersMappedPersonAndIgnoresUnmappedAndAgents` |
| T2 | **Fixed Nov 22.** `TicketChanged` carries optional `Criteria`, `View` folds updated criteria and sets `CriteriaEdited`, and `rowWriteTests` re-opens with a `tests` task naming uncovered criteria. Live Jira edit run not verified yet. | done | `internal/domain/change/events_core.go`, `view.go`, `rows_tests.go` | — | `TestTicketEditAddingCriterionReopensWriteTestsRow` |
| T3 | **Addressing policies were never verified.** Who may wake which agent in which room (F5) is designed but spike SI2 never ran; agents may answer any mention, and agent-to-agent mentions cost tokens. | medium | `AGENT_INTERACTION_ARCH.md` 257-258, 283-285 ("Not verified (spike SI2)"); `BRAINSTORM_BACKLOG.md` 1140 | Run SI2 on Discord and Mattermost; set the policies in the installer and record the result | A live check per platform (Phase 2 suite) |
| T4 | **A long think looks like a stall.** Stall is "no Heimdall tool call by this agent for 10 min"; a reviewer reasoning 10 min before its first call is woken again, which restarts its turn. | medium | `liveness/next.go` (`LastCall` only); H11 D6 (8,000 tokens of reasoning, no call) | Count the agent's own progress posts and OpenCode activity (via Switch's agent status) as signs of life, or raise the stall window per kind | Liveness test: an agent with no tool call but recent activity is `Wait`, not `Rewake` |
| T5 | **One agent, one task at a time, with no queue view.** A seat assigned a second change while busy keeps both tasks live; the second waits, its deadline runs, and nothing says it is queued. | medium | PROGRESS (Known issues: "the test agent works one ticket at a time"); `tasks.go` assigns without checking the agent's other live tasks | Assign a second task to a busy seat as `queued` (no wake, no deadline) and wake it when the first ends; `Missing` says "queued behind KAN-x" | Reconciler test: two changes for one seat: the second task is queued, then woken when the first is done |
| T6 | **A new agent's first start empties its folder.** Fixed in the activation script (restore and restart), not verified live; the root cause is in the Switch controller. | medium | PROGRESS H13 D176; `6becf8e` | Verify on the next new agent; ask Switch for a config template option | Live check when the next agent is added |
| T7 | **Telegram threads only exist in forum topics.** Thread replies, the change's thread and `@seat` routing differ there; not documented for users. | low | `AGENT_INTERACTION_ARCH.md` 283-285 | Document per platform; route Telegram replies to the change's room | Phase 2 cross-platform suite |
| T8 | **An edited chat message is not re-read.** A person who fixes a typo in a command by editing gets nothing; the platform sends an edit event Switch may not forward. | low | not verified (no edit handling in `roomstream`) | Decide: re-read edits of command messages within 5 min, or say "send it again" | Stream test with an edit delivery |

## 4. Order of work

1. T2 (ticket edits) and T1 (the lead): the two triggers a person meets first.
2. T5 and T4: queueing and true liveness, which also cut wasted tokens.
3. T3 (SI2) and T6 (verify the D176 restore) live.
4. T7, T8.
