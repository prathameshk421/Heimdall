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
| U1 | **Asks are never posted.** A person learns they are needed only by reading status or being told by an operator. | high | narrator posts three event types only (`narrator.go:277`); owner experience above | Post each new Ask once to its people (mention them) in the change's room and on the PR when it is a GitHub action; edit the post when it is done | Narrator test: an Ask appearing in `Missing` produces one post naming its people; a second run posts nothing |
| U2 | **Long ids.** Profile commands need the 64-hex proposal id. | medium | `profileNext` asks | Accept a unique prefix of 7 or more hex characters; show the short form in asks and replies | Command test: `approve-profile 49423b5` approves the proposal whose id starts with it; an ambiguous prefix is refused with the matches |
| U3 | **Long steps are silent.** Trials, check runs and verification run for minutes with no message; people act too early (D186) or wait without knowing. | medium | owner promoted before the trial | Announce `profile_trial`, `run_checks` and verification start and end in the room (one post each, edited) | Narrator test per long job kind |
| U4 | **Internal ids reach people.** The PR comment header shows the stream id. | low | PROGRESS Noticed H11 | Show the ticket key, or "PR #15" for a ticketless PR | Render golden for a ticketless PR |
| U5 | **The status "Next" line can lead with setup work.** Row needs come before the gate's needs; the repository's profile can appear before the PR's own approval. | low | status goldens after D175 (`testdata/plan/view/*/status.txt`) | Order needs by who must act and whether they block the merge | Render golden: a PR needing approval lists it first |
| U6 | **No one to ask in plain words.** `@heimdall why is this blocked?` gets no answer. | high | `agent-triggering.md` T1 | see T1 | see T1 |
| U7 | **The web board is unverified as a user surface.** It exists (`internal/transport/dashboard`), product-build adds sign-in; no person has used it for a real change. | medium | PROGRESS (Nov 7 dashboard merge); product-build merge `5c35ea3` | Phase 2: drive one change from the board alone | Phase 2 |

## 5. Order of work

1. U1 (post the asks): the single change that would have removed every relay the owner needed on Nov 21.
2. U3, U2: timing and typing.
3. U6 with T1; U7 in Phase 2.
4. U4, U5.
