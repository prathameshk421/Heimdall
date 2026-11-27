# Harness root causes: what the live runs broke, why, and the architectural fixes

From the live runs KAN-24 and KAN-25 (Nov 16 to 17, 2026) on the overhaul build. Evidence and timelines are in
`docs/archive/hackathon/LIVE_RUN_KAN24.md`; the record of every agent tool call is the `tool_call` table on the VM. This file is the
thinking: the bug log, the few causes underneath the bugs, and the changes that remove the causes rather than the
symptoms.

## 1. Bug log

| # | Bug (seen live) | Effect | State |
|---|---|---|---|
| B1 | `get_task` carried references only: the ticket, each criterion and the repo layout cost 15 calls | test author took 6 min to start | patched (`4b71443`) |
| B2 | the test author's `expected_paths` were dropped between `submit_tests` and the event | slop `scope` could never pass; **no reviewers were ever picked** | patched (`7769242`) |
| B3 | the placeholder-test check judged files, Go judges packages | a correct submission rejected; one wasted round; tests restructured to please a wrong rule | patched (`df6cc48`) |
| B4 | the verifier skipped if the change had *ever* had a verdict | every resubmission after a rejection was never verified; KAN-25 stalled 12 h | patched (`2284011`) |
| B5 | nothing notices "submitted, no verdict, nothing pending" | B4 was silent: no event, no log line, no message | **open** |
| B6 | `run_check` calls a runner job that M1 removed; the M3 replacement was never wired | every reviewer's `run_check` fails (4 of 4); turns wasted | **open** |
| B7 | renaming `agent` to `seat` made every older policy unreadable under strict decoding | card proposals silently off for 3 refs; a policy PR read as "policy invalid" | **open** |
| B8 | the board narrates events; a ticketless PR merged over a red gate was announced as a merge | Telegram showed "merged" while no ticket had merged | patched (`4b71443`) |
| B9 | the record's redaction hides SHAs, test names, then test file paths | the audit record is hard to read | half patched |
| B10 | card proposals log a warning and carry on when the policy does not decode | a feature is off and nobody is told | **open** |
| B11 | a reviewer's obligations included refund-cap and idempotency cards for a list endpoint | two of three answers are "not applicable"; noise in the brief | **open** |
| B12 | reviewer-3 went silent at 06:09 and nothing reacted | a seat can hold a change until its 30 min deadline | **open** (`liveness.Next` exists, not wired) |
| B13 | the test author still read the description with `read_untrusted` after it was inline | one redundant call; skill text and tools disagree | **open** |
| B14 | the gate's status stalls between pushes (KAN-22 note, still true) | the check says "reviews not known" long after they are | **open** |

The B-numbers that are "patched" were fixed one at a time as they appeared. That was the wrong mode: each fix was
correct, and each next bug was the same kind of bug in another place. The rest of this file is about the kinds.

## 2. The causes underneath

### C1. Data is copied by hand across every boundary, and fields fall off

`submit_tests` validates a `testset.Submission`; the agenttools service then builds `change.TestsSubmitted` field by
field, and `expected_paths` was simply not copied (B2). The envelope holds refs, and every consumer re-reads blobs
(B1). The intake re-derives what the test author already said. Each boundary is a hand-written mapping, so each
boundary is a place to drop a field silently, and no test notices a field that is missing, only one that is wrong.

### C2. Each executor decides "am I done?" with its own scan of the log

`test_quality` asked "is there any `tests.verified` or `tests.rejected`?" (B4); the other executors ask "is there any
`room.opened`?", "any `branch.ensured`?". This is exactly the bug class the state machine removal was meant to end, in
a second place: per-component interpretations of the log instead of one fold. The `View` already knows whether the
current submission is judged; the executor did not ask it.

### C3. The flow is edge-triggered, so a missed edge is a permanent stall

The engine reacts to events: an event appends an effect, the effect appends an event, and so on. If any step returns
"nothing to do" by mistake (B4), drops a field so a rule can never pass (B2), or an agent goes quiet (B12), the chain
stops and **no component is responsible for noticing**. `Missing` can already say exactly what the change needs; nothing
uses it to drive the work. Stalls are silent by construction (B5, B10, B12, B14).

### C4. Modules were merged in halves

M1 removed the static runner job (its step 2); M3 step 2, which moves `run_check` onto profile check rows, was not done.
Both halves were merged, every unit test passed, and the tool every reviewer is offered can only fail (B6). Nothing
checks that an offered tool can actually run against the real runner, and the tool list is not derived from what is
wired.

### C5. Tests cover the happy path; the retry paths are untested

B3 and B4 both live in "the author fixes and resubmits"; B2 in "a PR touches product code" (every real PR). No test, and
no scripted run, went through reject, resubmit, verify, commit, PR, review. The unit tests were green the whole time.

### C6. Configuration changes have no version

A strict decoder is right (a typo must fail), but the policy is read at the merge base, so old bases live forever. A
rename without a version breaks them all (B7), and the readers that tolerate failure hide it (B10).

### C7. People hear about events, not about state

The board posts one line per event through 18 narrators. A person sees "merged" for whatever merged, "PR received"
for any PR, and never sees "stuck: slop scope fails" because that is the absence of an event (B8, B14). F1 and H7
already say the board is `Render(View, Missing)`: one status per change, kept current. It is built (`announce.Render`)
but not wired, because Switch has no edit operation for agents (spike SI1).

### C8. The brief is assembled, not designed per kind

The reviewer's obligations come from a ranker that does not scope cards to the change (B11); the test author's skill
text still tells it to read what `get_task` now holds (B13); reviewers each re-read the 11.7 KB diff blob that could be
in `get_task`. The kind table (`workflow/kinds.go`) is where a kind's inputs should be declared, and it only declares
words.

## 3. Architectural fixes (one per cause)

### A1. One schema per kind, end to end (C1)

The kind table names each kind's output schema. The submission an agent sends, after validation, **is** the stored
fact: `tests.submitted` carries the whole validated `TestSubmission` (or the blob ref of it plus the fields the fold
needs, generated from the same struct), and nothing re-maps it field by field. A round-trip test per kind: submit, fold,
and the view holds every field the schema has. Adding a field to a schema then reaches every consumer or fails a test.

### A2. Executors and steps ask the View, never the raw log (C2)

`change.Fold` is the only interpreter of events. An executor's idempotency is a predicate on the view
("`Tests.Verified == nil && len(Tests.Rejected) == 0` for the current submission"), written once next to `Missing`.
Delete `loadedChange.has/first/last`; a lint rule (archcheck) refuses `range events` with a type switch outside the
change package.

### A3. Level-triggered orchestration: a reconciler over `Missing` (C3) — the central change

Replace "event causes effect causes event" with one loop, the way a Kubernetes controller works:

```
every 30 s and after every append, for each open change:
    view := Fold(events); needs := Missing(view, policy)
    for each need:
        owner := the kind table's owner of that row (server, agent kind, person)
        server-owned (verify tests, commit, pick, gate):  ensure exactly one pending effect exists; if none, create it
        agent-owned (write tests, review):                ensure a live task exists; liveness.Next decides nudge/reset/fail
        person-owned (approve, sign off, answer):         ensure the status says so; questions.Due decides reminders
```

Properties that fix the bugs as a class:
- A missed edge cannot stall a change: the next tick sees the need and re-creates its action (B4, B5).
- A rule that can never pass is visible: the need stays, and its text is in the status ("the slop rules pass on
  6b30fe4 (failing: scope)") for as long as it is true (B2 would have been obvious in a minute).
- One place decides what happens next. The engine's per-step wiring, the nudger and the timer loop collapse into it.
  `liveness.Next` and `questions.Due` (built) become its agent and person branches.
- Effects stay idempotent by key, so "ensure exactly one" is a lookup.

This is what F1 rule 3 implies and what "stupid simple orchestration" means in code: state is the fold, intent is
`Missing`, and one loop closes the gap.

### A4. Tools exist only when they can run; a self-check proves it (C4)

The kind table lists each kind's tools (F2 already says so). At boot the server runs each offered tool's dependency
once against the real adapter (`run_check`: ask the runner for its check list; `read_code`: read one file) and refuses
to offer a tool whose dependency fails, logging why. A contract test per tool against the runner's real HTTP API (the
runner is in this repo). `run_check` takes a profile `check_id` (M3 step 2) or is not offered.

### A5. A scripted end-to-end story is part of `make verify` (C5)

`cmd/heimdall-testagent` already plays agents. One story drives the real server, Postgres and runner with fake GitHub,
Jira and Switch through: trigger, a rejected submission, a resubmission, verify, commit, a PR touching product code,
three reviews (one with a finding), a push during review, re-review, approval, merge. Every live bug becomes one step
of it before its fix. The flow is then proven, not just its functions.

### A6. Policy carries a version; old versions convert (C6)

`version: 2` is the seats shape. The decoder reads each version strictly by its own struct and converts older ones in
one tested function. A reader that cannot decode does not degrade silently: it says so in the status of every change it
affects (B10).

### A7. The board renders state (C7)

Add the `update_message` operation to Switch (a small patch: the bridges already have `update_rich`); then the board
keeps one status message per change, `Render(View, Missing)`, as the thread root. Milestone narrators are deleted. The
merge line (B8) and the stuck line (B2, B14) come from the same view, so they cannot disagree with it.

### A8. The brief is declared per kind (C8)

The kind table names each kind's inputs: test author gets the ticket text, the repo map, the seam and the tests folder;
a reviewer gets the diff inline (bounded), the files changed, the obligations scoped to the changed files (cards whose
`applies_to` matches), the leads. Skill text is generated from the same row, so it never tells an agent to fetch what it
already has. The `tool_call` record gives a number per task (calls, refusals, minutes); a regression in it is a failed
check, not an anecdote.

## 4. Reviewer agents: what to change so a review is smooth

From KAN-25 (reviewer-1 clean in 7 min, 12 calls; reviewer-2 and reviewer-3 still working at 06:18; reviewer-3
silent since 06:09):

1. `run_check` works or is not offered (A4). Today it fails every time and each reviewer tried it.
2. The diff and `files_changed` are inline in `get_task` (A8); all three reviewers spent their first call on the same
   11.7 KB blob.
3. Obligations are scoped to the change (A8); two of reviewer-1's three cards were about refunds.
4. Leads exist only when M3 step 2 runs the checks; until then the brief says there are none rather than nothing.
5. A silent seat is handled by the reconciler's agent branch (A3 with `liveness.Next`): probe, reset, fail with a
   named need, instead of waiting for the 30 min deadline.
6. A submitted review closes its task (today `review.submitted` carries no task id, view README Q3); then "no active
   task" after a submit is not a refusal the reviewer can trip on (reviewer-1 called `get_task` after submitting).

## 5. Order

1. A3 (the reconciler over `Missing`, absorbing the nudger and timers) and A2 (executors ask the view). These remove
   the stall class, which cost the most time in both runs.
2. A4 and M3 step 2 (`run_check` on profile checks), so reviewers have their tools.
3. A1 (one schema per kind, round-trip tests) and A8 (inputs per kind, scoped cards, inline diff).
4. A5 (the scripted story in `make verify`), with B2 to B5 as its first steps.
5. A6 (policy versions) and A7 (Switch `update_message`, then the board).

Each is a small amount of code that deletes more than it adds: the engine's per-step chaining, the nudger, the timer
loop, the 18 narrators, the per-executor log scans.
