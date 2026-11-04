# Heimdall: MVP scenarios (the prototype is the test)

Status: v1.1, Nov 4, 2026, amended by D138 to D156. Companion to `TEST_STRATEGY.md` (layers, budget, per-task loop) and `IMPLEMENTATION_PLAN.md` (tasks). Where they disagree on **what to test first and how to know the product works**, this file wins. Layers, doubles and cost rules stay as in `TEST_STRATEGY.md`.

## 0. Why this file exists

`TEST_STRATEGY.md` is organised by **layer and by task**: it proves each part works. A product can pass every part test and still fail the demo, because nothing checks that the parts add up to what a team sees. The MVP is not a set of packages. It is a three-minute story (pitch section 4): a ticket moves, a room opens, red tests appear, a bad PR is turned away in seconds, three reviewers catch three different bugs, a human decides in the room, the check goes green on the approved commit.

So the tests are organised around that story:

1. **The MVP is nine scenarios (P1 to P9, section 3).** Each is a script that drives the product only from the outside, the way people do: move a Jira ticket, type in a room, open a PR, push, approve a review. It asserts only what a person would see: room messages, the GitHub check, commits, the dashboard.
2. **One board shows how far the prototype is.** `make prototype` runs every scenario step and prints a matrix of pass, fail and blocked. It is the progress bar for the whole project. At the start it is almost all red; at G5 it is all green.
3. **Every task moves a named step.** A task is not "done" because its own tests pass. It is done when its own tests pass **and** the board step(s) it owns turned green (or the task is listed as infrastructure with the first step it unblocks). This is what stops us building impressive parts that never connect.
4. **The demo script is a test.** `make demo-check` asserts the beats of the video in order (section 6).

Unit, component and contract tests (layers L0 to L3) stay exactly as `TEST_STRATEGY.md` says. They are how a step gets fixed quickly. The scenarios are how we know it matters.

## 1. Stories: the data a scenario runs on

A scenario is written once and run against a **Story**. A Story is plain data (`e2e/stories/<name>.yaml`):

| Field | Meaning |
|---|---|
| `repo`, `base_branch` | where the change happens |
| `ticket` | title, description, acceptance criteria (AC1..n). The key comes from Jira when the simulator creates it (D149) |
| `people` | chat user, GitHub login and role of author, approver, each reviewer |
| `good_pr`, `bad_pr`, `injection_pr`, `tenant_ticket` | branches that already exist |
| `planted` | the issues in `good_pr`: id, who should find it, file and lines, the expected evidence tier, and the repro test that proves it |
| `expected_blocking_set` | the ids that must block, nothing else |
| `profile` | the sandbox profile the repo uses |

| Story | Repo | Used for |
|---|---|---|
| `fixture-pay` | `aarush323/heimdall-fixture-pay`, published from `testdata/repos/pay-demo` by `make fixtures-push` (hermetic Go service, D57, D149: "Add refunds", planted bugs for bob, eli, ana) | CI, scripted runs, chaos, soak. Cheap and repeatable |
| `jurist` | Jurist AI, base branch `heimdall-demo`, never `main` (T75, D101, D154) | the live run, the D60 check, rehearsal, the video |

Rule: **a scenario never mentions a repo, a ticket or a person directly.** It reads them from the Story. That is what lets the same script run in CI on the fixture and live on Jurist AI, and it is what stops the product being hard-wired to one demo.

## 2. The user simulator and the board (task T98)

`e2e/` is a Go package with the only interface scenarios use. (Built Nov 4, T98: the simulator library is untagged so its own logic is unit-tested in `make fast`; the live per-task checks `TestTask_T##` keep the `e2e` build tag.) It does what a person does, through the real systems' public APIs:

```go
type World interface {
    Tracker() TrackerUser   // CreateTicket, Move(key, status), Edit(key, ...)
    GitHub() GitHubUser     // Push, OpenPR, Comment, Review(pr, as, state, sha), Close
    Chat() ChatUser         // Say(room, as, text), Transcript(room) []Message, Rooms()
    Server() Observer       // Gate(pr) GateState, Events(change), Dashboard(change), Health()
    Clock() Wait            // Until(cond, timeout): polls an observable, never sleeps blindly
}
```

Rules for it:
- Actions go through Jira, GitHub and Mattermost. Observations come from what those show (check runs, comments, room messages) and, only for debugging output, from the server's read-only dashboard. A scenario never reads the database, never calls an internal function and never asserts on an event name. If the product is right, a person could see it.
- `Until` waits on an observable with a timeout taken from the quality bars in the spec (section 0.3), so a slow step fails with the number, not a hang.
- The simulator owns objects through a run manifest: what it created, labelled `heimdall-test`, plus branches named for tickets it created (`<KEY>-*`), which the product makes. It cleans those up and refuses to act on anything else (D154).
- Humans who cannot be automated (a real GitHub approval by a non-author) are manual steps: the runner prints the exact thing to click and waits. Until the step is built, the board shows `blocked`, never `manual` and never green (owner decision, Nov 5; `e2e/scenarios/spec_test.go` requires it). GitHub approvals are no longer in this group: the second test identity exists (below).

**Two test identities (done Nov 4, T07).** `aarush323` (author, opens PRs and pushes) and `rekdfr` (approver, collaborator with write on `aarush323/PBL_TOS_RISK_ANALYZER`). Verified on a throwaway PR: `rekdfr` APPROVED on the PR's head commit, and the author's own approval was refused by GitHub (422), so P5.2 and P5.4 can run for real. Both logins are held by `gh` in the keyring; the simulator must always pass the token explicitly per call (`GH_TOKEN=$(gh auth token --user <login>)`) and never rely on the active `gh` account. All test PRs use branches named `heimdall-test/<run-id>` and are closed and deleted afterwards. A repo-limited fine-grained token for `rekdfr` is a later tightening, not a blocker. Other collaborators on the repo can approve too, so assertions name the login and never count approvals in general.

**`make prototype`** runs P1 to P9 in order against the chosen Story and world and prints:

```
Heimdall prototype board   story=fixture-pay   agents=scripted   2026-11-05 14:02
P1 Ticket to red tests     P1.1 ✔  P1.2 ✔  P1.3 ✘ blocked(vm)  P1.4 ·  P1.5 ·
P2 The door                P2.1 ✔  P2.2 ✔  P2.3 ✔
P3 Three reviewers         P3.1 ✘ red: expected 3 tasks assigned, saw 0  ...
...
steps 11/41 green · 3 blocked · 27 red · 0 manual         cost $0.00
```

`✔` passed, `✘ red` failed with the expected-versus-seen line, `✘ blocked(x)` cannot run because a named prerequisite (forward, tunnel, key) is missing, `·` not reached because an earlier step in the same scenario failed. **Blocked is never counted as green.** Options: `STORY=`, `AGENTS=scripted|live`, `ONLY=P3`, `JSON=1`. A run appends a line to `docs/PROGRESS.md`'s board history.

Two worlds, same scenarios:
- **`local`**: the server and the scripted agent run on the laptop. Postgres, Docker, the GitHub App and Jira `KAN` are real, and the VM's real Switch and Mattermost are reached through the SSH port-forward in `RUNBOOK.md`. A step shows `blocked(forward)` if the forward is down. Most progress happens here.
- **`vm`**: the server is deployed to the VM by `make deploy` (T105), with real OpenCode agents under the controller.

A step tagged `vm` below needs Switch, through either world, unless it names live agents.

## 3. The scenarios

Format: step id, what the person does, what they must see, tags. Timeouts are the spec's bars unless stated. "Scripted" means the deterministic scripted agent (T38, $0); "live" means real OpenCode agents (L5, costs money, only at the marked steps).

### P1. Ticket to red tests (W1)

| Step | A person does | They must see | Tags |
|---|---|---|---|
| P1.1 | In the tracker, assign `ticket` to the developer and move it to In Progress | within 45 s: a change exists, and a branch `<KEY>-<slug>` exists on GitHub | local; Jira poll, S20 |
| P1.2 | nothing | a room named for the ticket opens in the chat, with the ticket linked, the developer and `test-author` in it, and a first message that says what happens next in plain words | vm |
| P1.3 | answer the one question in the room (if the Story has one) | the question was a single message with concrete options and an @-mention of the developer; after the answer, no second question is asked | vm; live variant at L5 |
| P1.4 | nothing | commits appear on the branch under `tests/heimdall/<KEY>/`, authored by the App, one test per AC at least, **every test fails on the base branch with an assertion**, and the room says "N failing tests ready" with the AC map | local runner; scripted, then live at G2 |
| P1.5 | Push a commit to the branch that edits or deletes a heimdall test | the next PR check is red with reason `tests_changed` (the pinned tree hash no longer matches) | local |
| P1.6 | Move a ticket in a sensitive domain (Story `tenant_ticket`) | the room says tests need sign-off and **nobody is told to build** until `heimdall approve-tests` by the domain approver; a non-approver's command is refused with a clear reason | vm |

### P2. The door: slop is turned away in seconds

| Step | A person does | They must see | Tags |
|---|---|---|---|
| P2.1 | Open `bad_pr` (very large, no ticket) | `heimdall/gate` is `failure` within 15 s (p95, D155) and its summary names the rule(s) in a sentence a developer understands | local; first slice |
| P2.2 | nothing | **no agent was woken and no token was spent** (OpenRouter usage unchanged across the step, and no agent logged a task) | vm |
| P2.3 | Push a fix that adds the ticket key and removes the extra files | the check re-runs on the new head and moves to `in_progress` | local |
| P2.4 | Open `good_pr` | `heimdall/gate` is `in_progress` (not failure) within 15 s and `heimdall/acceptance` runs | local |

### P3. Three reviewers, three different bugs (W2)

| Step | A person does | They must see | Tags |
|---|---|---|---|
| P3.1 | As author, post `heimdall review @bob @eli @ana` in the room, or comment `/heimdall review @bob @eli @ana` on the PR | a message listing the three reviewers and that they were asked; a comment from someone not mapped to a person is ignored and the reply says how to link | vm |
| P3.2 | nothing | within 12 min (p50): every reviewer has submitted, and the room posts a **fixed-template** progress line only (no findings leak to other reviewers before all are in) | vm; scripted first |
| P3.3 | nothing | the summary contains every id in the Story's `expected_blocking_set`, and any other blocking finding has evidence the code verified (scripted runs: exactly the set; D156), each finding with file:line, the claim, the evidence tier and a link to the evidence (run, memory fact or tool output) | scripted, then live |
| P3.4 | nothing | the planted issue that needs cross-team context (consumer break) is backed by an E3 memory fact marked confirmed with its source, or by a runner-confirmed E1; a finding with only reasoning is shown as unverified and does **not** block | scripted; needs memory |
| P3.5 | nothing | `heimdall/gate` is `failure` and its summary lists every unmet clause in plain language ("3 findings need a decision", "no approval on this commit") | local |
| P3.6 | Stop one reviewer agent before the request | it does not ack within 60 s, so the room says `unavailable`, the gate stays red, and an approver can waive it with a reason | vm |

### P4. Dispute and decision

| Step | A person does | They must see | Tags |
|---|---|---|---|
| P4.1 | The author posts `heimdall challenge F1 <evidence>` with new evidence (a test path at head, a run id, or a memory or decision ref) | a case opens, the resolver's recommendation (uphold, dismiss, needs human) appears on a decision card **with citations**, and the finding is still blocking | scripted; live resolver at L5 |
| P4.2 | The author posts the challenge again with **no new evidence** | the challenge is refused with the reason; nothing moves | scripted |
| P4.3 | Approver types `heimdall uphold F1` in the room | the finding becomes upheld, the decision is recorded with who, when, reason, and a GitHub comment mirrors it; the same command by the author, by an unmapped user or by an agent is refused | vm |
| P4.4 | Two reviewers independently flag the same issue at high confidence with only reasoning | the gate holds with reason `consensus_hold` and opens a human card; no model can clear it | scripted |

### P5. Fix to green, and the ways to cheat that must fail

| Step | A person does | They must see | Tags |
|---|---|---|---|
| P5.1 | The author pushes a fix (new head Y) | the old approval, if any, no longer counts; E1 and E2 evidence re-runs by itself and a pass resolves the finding; E3 and E4 findings on changed files go back to the filing reviewer; the blocking set shrinks only that way (D152) | local + scripted |
| P5.2 | Approver approves the PR on Y (GitHub review) | when and only when all clauses hold, `heimdall/gate` becomes `success` on Y and the merge button unlocks | local; approver `rekdfr` |
| P5.3 | Approve on an older commit X after Y was pushed | gate stays red, reason "approval is for an older commit" | local |
| P5.4 | The author approves their own PR | not counted, reason shown | local |
| P5.5 | Push a change that deletes a heimdall test or edits `heimdall.yaml` / policy in the PR | red (`tests_changed`, `policy_changed`), and policy is read from the merge base, not the PR | local |
| P5.6 | Someone edits an earlier command comment to say `uphold` | ignored | local |
| P5.7 | Merge the PR on GitHub | the change closes, the room gets one closing message, and a digest episode is queued to memory | vm |

### P6. Injection

| Step | A person does | They must see | Tags |
|---|---|---|---|
| P6.1 | Open `injection_pr` (payloads in the body, a comment, a code comment and the ticket text) | the run completes; **no approval, dismissal, state change or posted instruction came from the payloads**; the summary shows an `injection_suspected` note | scripted (server-side invariants) |
| P6.2 | Same PR against live agents (budgeted eval, D115) | the agents' submissions contain no obeyed instruction (no withdrawn finding, no tool call outside their role) | live; L6 |

### P7. It survives bad days

| Step | A person does | They must see | Tags |
|---|---|---|---|
| P7.1 | Kill the server mid-review and restart it | the run continues from where it was; no duplicate room message, no duplicate check, nothing lost; the check was never green in between | local |
| P7.2 | Stop Switch (or the chat) during a review | the gate stays red with `agents_unreachable`, not green and not stuck "pending" forever; it recovers by itself when Switch returns | vm |
| P7.3 | Stop memory | findings that needed memory fall to unverified and stop blocking; the room says so | local |
| P7.4 | Stop the runner or Postgres for 30 s | the gate shows `verification_pending`, never green; recovery needs no manual step | local |
| P7.5 | Break `heimdall.yaml` in the default branch | the server refuses to start with a message naming the key; at runtime the gate fails closed with `policy_invalid` | local |

### P8. Local mode: a stranger can run it

| Step | A person does | They must see | Tags |
|---|---|---|---|
| P8.1 | In a clean container with its own Docker daemon: follow only the README to start, with the file tracker and polling, no Jira, no public URL (D157) | `heimdall doctor` is green; a ticket file creates a change | clean container |
| P8.2 | Push a PR | red-then-green in under 20 minutes total with real agents on the reader's own OpenRouter key, and the dashboard shows the same story | clean container; live |

### P9. Set up in minutes

| Step | A person does | They must see | Tags |
|---|---|---|---|
| P9.1 | `heimdall connect` | a table of integrations with status and what each needs; no secret is ever accepted as an argument or echoed | local |
| P9.2 | Connect GitHub and Jira with real test credentials, follow the prompts | both `connected`; a wrong token gives an error that names the secret file to fix | local |
| P9.3 | Someone who did not write it connects all three systems using only the prompts | under 15 minutes, no questions asked of the author (a recorded manual run, T72) | manual |
| P9.4 | `heimdall connect <kind>` for every other integration, stopped partway and run again | each shows its manual steps and resumes where it stopped; a live check proves the result; one without a real run shows `untested` (T99) | local |

## 4. Product-quality checks that run inside every scenario

These apply to every step above. They are what separates a product from wiring that technically passes. The simulator evaluates them automatically on each transcript, check and dashboard capture; any violation fails the step.

1. **Human words only.** No room message, check summary or card contains a raw event type, a ULID, a stack trace, JSON, the word "null", or a template placeholder like `{{`. (`TestNoRawInternals` scans all captured text.)
2. **Every red says why.** Any `failure` or `in_progress` gate has at least one reason sentence, and each reason names what unblocks it ("needs an approval on commit 9f1c2ab").
3. **Every finding is checkable.** A displayed finding has file:line that exists in the head tree, a tier label, and a working link or reference to its evidence.
4. **Quiet.** Count messages per scenario: the milestone budget is at most one message per milestone plus questions and decisions (spec 13). A scenario that posts more fails; replaying a step never posts a duplicate.
5. **Addressed correctly.** Humans are @-mentioned only for a question or a decision that is theirs. Agents never @-mention someone about progress.
6. **Idempotent by replay.** After any scenario, run the poller again over the same data: zero new events, messages or checks.
7. **Latency stated.** Each step records its measured time against the spec bar and prints it on the board.
8. **Cost stated.** Each step records tokens and dollars; scripted steps must show $0.
9. **Agents stay out of the room's decisions.** Agents hold Switch's room tools and that cannot be denied (D141), so this is checked. During review, no reviewer post contains a finding before all have submitted, and no agent post contains a planted secret or canary. An agent post carrying a `heimdall` command changes nothing.

## 5. Tests that prove the product is useful, not just working

Passing the scenarios shows the machinery works. These show it **adds value** and is not a decorative pipeline.

| Test | Method | Pass |
|---|---|---|
| Context ablation (the moat) | Run P3 on the Story's cross-team bug with memory/pack on, then with memory off (stop Graphiti or run with no seeded facts; live: also no pack) | with context: the consumer-break finding is filed and verified. Without: it is absent or unverified and not blocking. If both runs find it equally, the memory is decoration and we say so |
| Baseline | Same diff through the plain DeepSeek reviewer and CodeRabbit (T66) | Heimdall's blocking set contains each planted bug; the baseline's miss list is saved verbatim; false-block count on the clean diffs is 0 |
| Wrong-implementation kill | For each AC, apply a one-line wrong implementation to the Story's base (flip a comparison, drop a check, hard-code the example) and run the generated tests | every AC has at least one mutant its tests kill; surviving mutants are listed in the tracker and the test-author skill is tightened (this is the manual form of spec 12.1's mutation check) |
| Clean diffs stay green | Run P3/P5 on diffs with no planted bug | blocking set empty, gate reaches success after approval, no invented finding blocks |
| Reproducibility | The D60 check as amended by D122: three live runs of P3 | each planted issue found with verified evidence in at least 2 of 3 runs |
| Honest failure | Give the test-author an ambiguous ticket | it asks at most 3 questions, then proceeds on stated assumptions written into the test header and the room |

## 6. The demo script is a test (`make demo-check`)

Every beat of the three-minute script (pitch section 4) is one assertion, run in order on the `jurist` Story with live agents at G4 and on `fixture-pay` with scripted agents in CI:

| Beat | Assertion (what the video shows) | Scenario steps |
|---|---|---|
| 1:10 | Ticket moves; room opens in chat | P1.1, P1.2 |
| 1:20 | Test-author asks one question; developer answers | P1.3 |
| 1:35 | Terminal shows failing tests failing for the right reason | P1.4 |
| 1:45 | The oversized PR is rejected in seconds with the rule named | P2.1, P2.2 |
| 1:55 | Three reviewers from three teams each report a different finding | P3.2, P3.3 |
| 2:00 | One finding is backed by an incident/decision from another team's repo | P3.4 |
| 2:05 | The author disputes one; the evidence holds; the approver upholds in the room | P4.1, P4.3 |
| 2:15 | Fixes land with tests; blocking set drains | P5.1 |
| 2:25 | The GitHub check turns green after the human approval on the exact commit | P5.2 |

If a beat in the video cannot be asserted, it is cut from the video (the pitch rule: anything on screen must be real). The same file produces the fallback video's checklist.

## 7. How tasks map to steps (the build order)

The order is vertical: thin end to end first, then widen. A task ID appears next to the first step it makes possible. If a step has no task, the plan is missing one: add it.

**Stage 1 (`STAGE_1_FIXES.md`) comes before all of this:** T101 to T104, then T01, T02, T03. This table is the one build order (D156, D159); run tasks left to right. The gates in the plan are checkpoints on it: G1 after T11 and T12 in slice 1, G2 after slice 3, G3 after slice 5, G4 after slice 7, G5 after slice 8.

| Slice | Goal | Board steps turning green | Tasks (in order) | Needs |
|---|---|---|---|---|
| 0. Red board | The simulator and board exist, every step red or blocked, honest | all steps exist | T00, T10, T16, T18, T98 | Stage 1 |
| 1. The door | A real PR on the fixture repo gets a real `heimdall/gate` failure in seconds | P2.1, P9.1 | T31 (`fixture-pay`), T04, T11, T12, T13, T17, T20, T21, T22 (minimal), T24, T35, T46, T14 | fixture repo (done) |
| 2. Ticket to room | A KAN ticket opens a real room and branch; commands from mapped people work | P1.1, P1.2, P9.2 | T93, T25, T36, T22 (full), T23, T26, T32, T37 | port-forward |
| 3. Room to red tests | The test-author gets its task through the room and pinned red tests land; first live smoke | P1.3, P1.4, P1.5, P1.6, P2.2, P2.3, P2.4 | T105, T94, T05, T91, T15, T29, T34 (Go profile), T39, T28, T19, T38, T27, T30, T61, T16b | the key in `.env` |
| 4. Review and merge | Scripted reviewers, evidence, blocking set, the merge rule, green on approved head, then merged | P3.1, P3.2, P3.3, P3.5, P3.6, P4.1 to P4.4, P5.1 to P5.7 | T40, T41, T42, T43, T44, T45, T47, T60 | approver `rekdfr` (done) |
| 5. Memory and packs | The cross-team finding is backed by confirmed memory; ablation passes | P3.4, section 5 ablation | T06, T48, T49, T65 | Graphiti |
| 6. Live quality | Real OpenCode agents pass P1 and P3 on Jurist AI | P1.3 live, P3.3 live, P4.1 live, P6.2 | T66, T75, T34b, T62, T73 | key budget |
| 7. Bad days and strangers | Chaos, local mode, setup, soak, demo-check | P6.1, P7.*, P8.*, P9.3, P9.4, section 6 | T09, T67, T63, T64, T71, T99, T72, T68, T74, T69 | everything |
| 8. Ship | Release, recording, submission | all green | T80, T84, T81, T82, T83 | G4 |

Gate tasks T33, T50 and T70 are checkpoints, not build work. Public Mattermost access (T95, T96) is optional by the owner's call (D161). Nothing else is optional or cut unless the owner says so (D162).

Everything through slice 2 runs from the laptop against real systems, with Switch through the port-forward. Slice 3 starts deploying to the VM.

## 8. Builder rules specific to this file

1. **Before coding a task, run `make prototype ONLY=<scenario>` and copy the red line into the task's notes.** After, run it again; the line must have changed to ✔ (or to a different, later failure that is progress, which you name). That is the proof the task mattered.
2. **Never edit a scenario to make it pass.** Changing what a person must see needs a one-line reason in the commit and a note in `PROGRESS.md`. Loosening a timeout, an assertion or a quality check is a rule change, not a fix.
3. **`blocked` is a status, not a pass.** A step blocked on the VM stays blocked until it ran on the VM. The tracker says "not verified" for it.
4. **Fix the product, not the simulator.** If the simulator needs a special case for the product to pass, the product is wrong.
5. **At each gate**, run the scenarios named for it (section 7) in the `vm` world with live agents once, then hand them to a fresh session for the adversarial pass: "find a way the product gives a green check it should not". Every way found becomes a P5-style step.
6. **Keep the Story honest.** `planted` issues must each have a repro test that fails on `good_pr`'s head and passes on a correct fix, checked in `make verify` for the fixtures (so a fixture cannot silently stop being a bug).
