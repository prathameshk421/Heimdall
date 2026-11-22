# Product function: the whole journey (SOTA Phase 0, Nov 22, 2026)

Scope: the complete journey on the real systems (Jira, Switch rooms, GitHub, the runner, the model provider) and every
branch on the way. Sources: `MVP_SCENARIOS.md`, `TECH_SPEC.md`, `HARNESS_DESIGN.md`, the code at `cffca6e`, and the live
runs: KAN-26 ticket to merge (H11, Nov 19 to 21), the profile lifecycle and PR #18 (H13, Nov 21).

## 1. Desired state

A team connects a repository, a tracker and chat once. From then on:

1. A ticket assigned to the flow opens a room, a branch and a test task; the tests fail on the base for the right
   reason and are committed.
2. The developer's PR is checked (slop, CI, acceptance, profile checks), reviewers are picked, reviews arrive with
   proven findings, challenges are settled, and the gate turns green only with a person's approval on the head commit.
3. GitHub merges; the ticket moves on in the tracker, with a comment linking the evidence; the room closes with a
   summary.
4. Every branch behaves predictably: a push mid-review, a force-push, a reopen, a second PR, a ticketless PR, an
   unknown ticket, a changed ticket, a sensitive domain that needs a person to sign off the tests, a challenge, a dead
   job, a quiet agent, a refusing provider, an out-of-order command.
5. A new repository gets a sandbox profile through the profiler, approved and promoted by a policy owner, and can
   revise it later.

## 2. Journey coverage

| Step or branch | Built | Verified live | Evidence |
|---|---|---|---|
| Ticket → room → branch → test task → red first → commit | yes | **yes** (KAN-26) | `LIVE_RUN_H11.md` |
| PR → slop → CI → acceptance → pick → three reviews | yes | **yes** (KAN-26 PR #16, PR #18) | `LIVE_RUN_H11.md`; PR #18 events |
| E1 and E2 verification | yes | **yes** (PR #18: 33 verified, 3 E2) | PROGRESS H13 E2 run |
| Gate → approval on head → merge | yes | **yes** (PR #16 merged `d79cd08`) | `LIVE_RUN_H11.md` finish |
| Reopen after close | yes (D8) | **yes** | `LIVE_RUN_H11.md` D8 |
| Ticketless PR reviewed, gate red | yes (D174) | **yes** (PR #18) | PR #18 events 2 to 6 |
| Push mid-review supersedes tasks | yes | **yes** (PR #18, three heads) | PR #18 events |
| Quiet agent: re-wake, stall wake, fail, cap | yes (D172, D173) | partly (stall wake yes; cap not hit live) | PR #18 reviewer-2 |
| Profile: propose, approve, trial, promote, revise | yes (D175, D184) | **yes** | PROGRESS H13 |
| Dead job → `heimdall resume` | yes (D188, test first) | not verified live with a person's `heimdall resume` | `a5f0a00` |
| Approval invalidated by a new head | yes | not verified live | `internal/app/prsync/prsync.go:614` |
| Sensitive domain: a person signs off the tests | yes (`signoff` row) | not verified live | `internal/domain/change/rows_tests.go:131` |
| Challenge → case → resolver verdict | yes | **never ran live** | no deliberation events (see `agent-reasoning.md` R1) |
| Force-push | not specified apart from a new head | not verified | — |
| Two PRs for one ticket | partly | not verified; a second PR after a closed first is folded into the ended change | `orchestration.md` O8 |
| Ticket edited after tests | **no** (no row reacts) | — | `agent-triggering.md` T2 |
| Tracker update after commit or merge | **no** (`tracker_write` has no producer) | — | no emitter of `EffectTrackerWrite` (grep) |
| Room summary and close after merge | not built (the room stays open) | — | no close effect in the rows |
| Repository onboarding (connect once) | manual: `heimdall.toml`, secrets, installer scripts, Switch console steps | done by hand on one VM | `RUNBOOK.md`; `deploy/` |

## 3. Gaps

| # | Gap | Severity | Evidence | Smallest correct fix | Test that would have caught it |
|---|---|---|---|---|---|
| F1 | **Fixed Nov 22.** `rowTrackerCommit` (after `commit`) and `rowTrackerMerge` (in `FlowMerge` after `pr.closed{merged}`) emit `tracker_write` jobs with optional `policy.Tracker.MergeStatus` transition; Jira and file adapters plus `TrackerWrite` executor wired. Live Jira run not verified yet. | done | `rows_merge_test.go`, `jira/write_test.go`, `file/write_test.go`, `effects/room_and_tracker_test.go` | — | `TestTrackerCommitRowEmitsTrackerWriteAfterTestsCommitted`, `TestMergedPREmitsTrackerWriteWithPolicyTransitionAndRoomClose` |
| F2 | **Fixed Nov 22.** `rowClose` (in `FlowMerge`) emits a summary `room_post` and a `room_close` job that archives the Switch room (`archive_room`) and records `room.closed`. Live Switch archive not verified yet. | done | `rows_merge_test.go`, `switch/close_room_test.go`, `effects/room_and_tracker_test.go` | — | `TestMergedPREmitsTrackerWriteWithPolicyTransitionAndRoomClose`, `TestCloseRoomCallsArchiveRoom` |
| F3 | **The resolver path has never run.** | high | `agent-reasoning.md` R1 | Phase 2 scenario with a challenge | Phase 2 |
| F4 | **Ticket edits are ignored.** | high | `agent-triggering.md` T2 | see T2 | see T2 |
| F5 | **Onboarding is a manual runbook.** Connecting a new repository needs config edits, secret files, installer scripts and Switch console clicks; a buyer cannot self-serve. | high | `docs/RUNBOOK.md`; `deploy/agents/README.md`; H13 provisioning (D176, D177) | A `heimdall init` that writes the config, checks every credential and runs prepare and activate; the board shows what is missing | End-to-end setup test on a fresh VM (Phase 2.5) |
| F6 | **Approval invalidation, sign-off and force-push are unverified live.** | medium | table above | Phase 2 scenarios | Phase 2 |
| F7 | **Second PR for one ticket.** | medium | `orchestration.md` O8 | see O8 | see O8 |

## 4. Order of work

1. F1 and F2: they close the loop a buyer sees first.
2. F3, F4, F6 in the Phase 2 suite.
3. F5 (onboarding) with Phase 2.5 sellability.
4. F7 with O8.
