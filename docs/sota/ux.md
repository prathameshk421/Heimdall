# The person's experience (SOTA Phase 0, Nov 22, 2026)

Scope: wording, timing, status, recovery, approvals, the board and every chat platform. Sources:
`AGENT_INTERACTION_ARCH.md` F1 to F6, the narrator (`internal/app/announce`), the command processor
(`internal/app/collaboration`), and what the owner actually had to do on Nov 21 (profile approve, promote and revise in
Discord `#heimdall-reviews`; PR #16 approval and merge on GitHub).

## 1. Desired state

- **Heimdall tells you when you are needed, once, where you are.** Every Ask (approve tests, approve or promote a
  profile, resume a dead job, approve a PR) is posted to the person who can act, in the room or thread they use, with
  a command that can be copied as is. Nobody needs to read a status page or ask an operator what to do next.
- **Every command gets a reply** saying what happened or why not, in plain words, within seconds (D186 applies to every
  command, not only profile ones).
- **Ids are short.** People type `F3`, `KAN-23`, `profile 49423b5`; never a 64-character hash or a stream id.
- **Status is one glance.** The PR comment and the room card say what the change waits for, in priority order (the
  merge work first), and who must act.
- **Nothing is silent for long.** A step that takes more than a minute (a trial, verification, a stalled agent) says it
  started and when it ends.
- **Every platform feels the same**, and where one differs (Telegram threads) the difference is stated.

## 2. What the owner met on Nov 21

| Moment | What happened | Evidence |
|---|---|---|
| Approving a profile | Had to ask which chat and channel; then `#heimdall-reviews` answered "Which ticket is that about?" to a command with no ticket key | thread, Nov 21; fixed by D193 (`a4733f2`) |
| Typing the command | A 64-character proposal id had to be pasted from the operator's message | the asks in `rows_review.go` `profileNext` use the full id |
| Waiting for the trial | No message said the trial ran or passed; the owner sent `promote` 11 s after approving, before the trial, and got no reply while the chat stream was blocked for 12 min | PROGRESS H13 (D186) |
| After promote | The reply came only once the runner fix was deployed | PROGRESS H13 (D185) |
| Following the work | Every next step was relayed by the operator (me), not by Heimdall | this thread |
| PR comment header | Shows the stream id `pr-aarush323-heimdall-fixture-pay-15` | PROGRESS Noticed H11 |

## 3. What exists

| Part | Behaviour | Evidence |
|---|---|---|
| Narrator | Posts on `task.assigned`, `pr.opened` and `change.merged`; keeps one PR review-board comment and one line comment per finding | `internal/app/announce/narrator.go:207,228,277` |
| Status | `Render(view, Missing)` gives a card: counts and a "Next" line | `internal/app/announce/render.go`; status goldens in `testdata/plan/view/*/status.txt` |
| Asks | Rows emit `Ask{Who, What}`; they appear in `Missing` (status, `get_task`) only | `internal/domain/change/rows.go`; no Ask poster in `announce` (grep) |
| Command replies | "Not done: …" for refusals; "Sorry, you are not allowed …" for unauthorized; usage for bad args | `internal/app/collaboration/commands.go:133-160` |

## 4. Gaps

| # | Gap | Severity | Evidence | Smallest correct fix | Test that would have caught it |
|---|---|---|---|---|---|
| U1 | **Fixed Nov 22.** Every row `Ask` now emits an idempotent `room_post` job mentioning its people (`@who`) in the standing channel or change room (`reconcile/decide.go`, `effects/room_post.go`). Edit-on-done and live run not verified yet. | done | `reconcile/ask_and_merge_test.go`, `effects/room_and_tracker_test.go` | — | `TestAskIsPostedOnceNamingItsPeopleAndSecondRunPostsNothing`, `TestAskRoutesToStandingChannelWhenConfigured` |
| U2 | **Fixed Nov 22.** `approve-profile` and `promote-profile` accept 7–64 hex prefixes (`commands/args.go`, `ports.ProfilePrefixResolver`, `postgres.ProfilesByPrefix`, `app/profile.resolveProfile` refusing ambiguous prefixes with matches), and `profileNext` asks show 7-char short IDs. | done | `internal/app/profile/service.go`, `internal/domain/change/rows_review.go` | — | `TestApproveAndPromoteAcceptUniqueShortPrefixAndRefuseAmbiguous` |
| U3 | **Long steps are silent.** Trials, check runs and verification run for minutes with no message; people act too early (D186) or wait without knowing. | medium | owner promoted before the trial | Announce `profile_trial`, `run_checks` and verification start and end in the room (one post each, edited) | Narrator test per long job kind |
| U4 | **Fixed Nov 22.** `announce.RenderBoard` and `announce.Render` display `PR #<n>` instead of raw `pr-...` stream IDs for ticketless PRs. | done | `internal/app/announce/board.go`, `render.go` | — | `TestTicketlessPRHeaderShowsPRNumberNotStreamID` |
| U5 | **The status "Next" line can lead with setup work.** Row needs come before the gate's needs; the repository's profile can appear before the PR's own approval. | low | status goldens after D175 (`testdata/plan/view/*/status.txt`) | Order needs by who must act and whether they block the merge | Render golden: a PR needing approval lists it first |
| U6 | **Fixed Nov 22 (with T1).** Mapped humans' `@heimdall ...` chat questions receive a factual status reply (`AnswerQuestion`); unmapped senders and agents are ignored. Live chat run not verified yet. | done | `agent-triggering.md` T1 | — | `TestMentionQuestionAnswersMappedPersonAndIgnoresUnmappedAndAgents` |
| U7 | **The web board is unverified as a user surface.** It exists (`internal/transport/dashboard`), product-build adds sign-in; no person has used it for a real change. | medium | PROGRESS (Nov 7 dashboard merge); product-build merge `5c35ea3` | Phase 2: drive one change from the board alone | Phase 2 |

## 5. Order of work

1. U1 (post the asks): the single change that would have removed every relay the owner needed on Nov 21.
2. U3, U2: timing and typing.
3. U6 with T1; U7 in Phase 2.
4. U4, U5.
