# Harness overhaul (D170): design and build plan

Status: **final**, Nov 17, 2026. Owner decisions after the KAN-24 and KAN-25 live runs. This file is the spec and the
plan; an agent with no other context executes it step by step (Part C). The *why* is in `docs/HARNESS_ROOT_CAUSES.md`
(bugs B1 to B14, causes C1 to C8, fixes A1 to A8); the live evidence is in `docs/LIVE_RUN_KAN24.md`. Under D138 this
file outranks `AGENT_INTERACTION_ARCH.md`, `BUILD_PLAN.md` and the parts of `TECH_SPEC.md` it contradicts (section 5
the workflow engine, 6.2 the task index, 12.4 how the gate is evaluated); step H12 rewrites those sections.

Every fact in Part B0 was checked in the source at commit `2284011` on `heimdall-build`.

---

## Part A. How to use this file (read first)

**Who executes.** One agent, steps H0 to H12 in order. A step may take several sessions; never start a step before the
previous one is committed. Each step lists *Goal*, *Read first*, *Create*, *Change*, *Delete*, *Tests*, *Done when*,
*Traps*. Do what it says. If the code contradicts a step, stop that step, write what you found under "Noticed" in
`docs/PROGRESS.md`, and ask the owner. Never invent a workaround (R13).

**Rules for every step** (from `CLAUDE.md`, restated because they bite here):
- `make fast` green before every commit; `make verify` before the commit that ends a step. Commit on `heimdall-build`
  as `H<n>: <what>` with the attribution lines the session gives. Never push, never `--no-verify`.
- **Delete what you replace, in the same step.** No flag, no "kept for now", no fallback path, no adapter keeping an
  old API alive. A test that only tests deleted code is deleted. A test of behaviour that survives is ported and keeps
  its assertions (R2); an assertion that must change because behaviour changed on purpose is changed, and the commit
  message says which and why.
- **Event log compatibility.** Stored events are never rewritten. `change.Decode` is strict (`DisallowUnknownFields`),
  so a payload struct may gain fields (`omitempty`) and never loses or renames one. A field no writer sets any more
  stays, with the comment `// Old events only.` Event types nothing writes any more stay in the catalog with the same
  comment, and the fold ignores them.
- **Domain purity (R5).** `internal/domain/*` does no I/O, reads no clock, makes no ids. Time arrives as a parameter
  (`In.Now`); ids come from `ports.IDs` in the app layer.
- **Dependency rule (R3, enforced by `internal/archcheck`).** `internal/app/<x>` may import only stdlib,
  `internal/domain/*` and `internal/ports` — **not another `internal/app` package**. Wiring lives in
  `cmd/heimdall-server`.
- **Lint traps seen before:** `time.Sleep`, `time.NewTimer`, `os.Getenv`, `fmt.Print*` are banned outside their homes
  (wait with `clock.Wait` from `internal/platform/clock`); `gocyclo` max 12 (split, use tables); `exhaustive` wants a
  `default:`; run `goimports`; no `go/parser` in `internal/app` beyond the allow list in `internal/archcheck/parser.go`.
- **Tests (R1).** Failing test first for real logic: rows, the reconciler, fold fields, gate facts, liveness, effect
  ensuring. No tests for wording, wiring or config.
- **Evidence.** At the end of each step update `docs/PROGRESS.md` (top section "Harness overhaul (D170)"): step,
  commit, `make fast`/`make verify` exit codes and test counts, what is **not verified** and why.

**Owner approvals already given (chat, Nov 16 to 17, 2026)** — quote them in commits that rely on them:
- changing assertions where behaviour changes on purpose ("Yes, start phase 2" — M0 phase 2);
- deleting replaced code with no fallback ("the old slop thats left pls delete ... clean code is key");
- retiring `e2e/stories` ("pls come out of the story thing ... we are trying to make it a real product");
- two decoupled flows, one schema per kind, the desired-state loop ("i agree with one scheme per kind", "a3 also
  agreed. desired state and current state the loop checks and fires off stuff");
- live runs on the VM with the owner's Jira credentials from `.env` (never printed), approvals from the `rekdfr`
  login, spend up to $3 per run.

**Still needs the owner** (ask, never assume): edits to `.golangci.yml`, `testdata/real`, `CLAUDE.md`,
`.gitleaksignore`; pushing anything; any spend above $3.

---

## Part B. The design

### B0. What the code does today

| # | Fact | Where | Consequence |
|---|---|---|---|
| K1 | The engine re-runs open steps (`Advance` loops until a pass stores nothing; the timer loop calls `Advance(nil)` every 15 s), but steps start work in `OnEvent`: `TestQA` emits its effect only when `tests.submitted` arrives. A missed edge is never retried. | `internal/app/engine/engine.go`, `steps_testqa.go`, `cmd/heimdall-server/timers.go` | B4, B5: a skipped verification stalls forever, silently |
| K2 | The log is interpreted five times: `change.Fold`; each step's `Done` (`ChangeView.Has`); each executor's `loadedChange.has/first/last/judgedSince`; `gateuse.foldState` (450 lines); and the SQL of `postgres.TaskIndex` (which events end a task). | `engine/steps_*.go`, `effects/effects.go`, `gateuse/inputs.go`, `postgres/taskindex.go` | C2: they disagree (B4); each rule exists 2 to 5 times |
| K3 | Two row tables: `workflow.Default()` (14 steps with `after`, `on_fail`; trimmed again in `cmd/heimdall-server/slice.go`) drives the engine; `change.rowTable()` drives `Missing`. | `workflow/definition.go`, `rows_*.go`, `change/missing.go` | a row can be done for one and missing for the other |
| K4 | PR intake appends `pr.opened`/`slop.evaluated` with a raw `Append`. The engine sees it only when the timer loop runs, and that loop visits only changes with a room (`RoomIndex.OpenRooms`). | `intake/intake.go`, `timers.go` | a ticketless PR (`pr-<repo>-<n>`, no room) is **never reviewed**; a ticket PR waits up to 15 s per step |
| K5 | Review is chained after the tests (`slop after wait_pr after commit`). A PR naming a ticket that has no change is refused (`intake.ErrNoChange`). | `rows_core.go`, `rows_review.go`, `intake.go` | the flows are coupled; a PR opened before its ticket moved is dropped |
| K6 | The gate is published three ways: intake's `gateEffect`, `prsync` calling `gate.Evaluate` on a new head, and `gateRefreshLoop` every 5 s on a hand-kept `movesGate` list. The pure rule `gate.Evaluate(gate.Facts)` is sound. `change.Missing` re-implements part of it (`gateNeeds`). | `intake.go`, `prsync/prsync.go`, `gateuse/refresh.go`, `domain/gate/gate.go` | B14: the check stalls; the status and GitHub can disagree |
| K7 | Retries are `step.retried` events plus per-step windows; escalations hold a step until `step.resumed`, which the existing `heimdall resume <KEY> <step>` command writes (`cmd/heimdall-server/resume.go`). | `engine/plan.go`, `onfail.go` | the hardest code in the repo; B4 hid there |
| K8 | An outbox row needs an `event_id` and is written only inside `Append`. `effect_key` is unique: inserting an existing key is a no-op when kind, target and payload are equal and an error otherwise. A row with `started_at` set, `done_at` null and an expired lease is "in doubt" and never claimed again; after 8 failed attempts a row is dead (`outbox_dead`). | `postgres/append.go`, `postgres/outbox.go`, migrations `0001`, `0002` | ensuring a job is an insert by key, if payloads are deterministic; dead and in-doubt jobs must be shown, not hidden |
| K9 | Nobody builds a profile `check` job. `run_check` and `verify_evidence` (E2) send the removed `static` job with a hard-coded `go_vet`; the runner refuses it. The profile holds checks (`ProfileStore.ActiveProfile(repo)` → `.Spec.Checks`, type `ports.ProfileCheck`); `sarif.Parse(data, check, lens)` and `sarif.New(base, head)` exist. | `mcp/tools_checks.go`, `effects/verify_evidence.go`, `rootlessrunner/api.go` | B6: every `run_check` fails; E2 findings can never be verified; reviewers get no leads |
| K10 | Tools are offered per role (`roles` on each tool); `workflow/kinds.go` holds only words. `approve_profile` and `promote_profile` are offered to the reviewer role. `get_task` returns a `TaskEnvelope` of references plus brief sections; `get_change`, `read_untrusted` and `ack` exist to fetch or confirm what `get_task` could carry. | `mcp/tools_*.go`, `mcp/brief.go`, `mcp/orient.go`, `workflow/task.go` | B1, B13: extra calls; tools, skills and kinds disagree; a reviewer agent can promote a sandbox profile |
| K11 | The nudger re-mentions agents on a doubling schedule; `liveness.Next` (wait, repeat, probe, reset, fail) and `questions.Due` are pure and called by nothing. | `cmd/heimdall-server/nudge.go`, `internal/app/liveness`, `internal/app/questions` | B12: a silent seat holds the change until its 30 min deadline |
| K12 | `review.submitted` and `tests.submitted` carry no task id, so the task index guesses which task a submission ends. `tests.submitted.tree_hash` is set to the base SHA. `pr.opened` carries no base SHA. | `change/events_*.go`, `agenttools/tasks.go` | a reviewer calling `get_task` after submitting is refused; a field lies; the base must be fetched |
| K13 | The resolver runs as an engine step (`deliberation.run`, `ResolverFanout`): one task per open case (`case.opened` until `verdict.applied` or `verdict.escalated`), never done. | `engine/steps_resolver.go` | `deliberate` must be a row, or disputes stop being resolved |
| K14 | Reviewer seats: `reviewers.picked.people` holds agent names. The default pick is the PR's CODEOWNERS suggestions (`pr.opened.suggested_reviewers`), else `config.Agents.Reviewers`; the author has 1 min, counted from the last `slop.evaluated`. | `engine/steps_pick.go`, `slice.go` | the pick row keeps this behaviour |

### B1. Two flows on one change

A **change** is one event stream, as today: a ticket's key, or `pr-<repo>-<n>` for a PR that names no ticket (D146).
A change carries up to two flows; each starts on its own fact and needs nothing from the other.

| Flow | Starts on | Rows | Ends when |
|---|---|---|---|
| **Tests** | `change.triggered` (a ticket moved to In Progress) | `branch`, `write_tests`, `test_qa`, `signoff`, `commit` | the verified tests are committed to the ticket branch; CI runs them on every push (`heimdall/acceptance`) |
| **Review** | `pr.opened` | `slop`, `checks`, `pick`, `review`, `verify`, `deliberate`, `acceptance`, `gate` | the PR closes |
| both | the first fact of the change | `open_room` | the room exists |

- `wait_pr` is deleted. Review rows wait only for a PR. A PR opened before its tests are committed is reviewed at once;
  its gate shows the unmet test clauses until the tests land.
- A PR naming a ticket with no change **starts that change**: `pr.opened` is version 1 of the ticket's stream. When the
  ticket later moves to In Progress, the trigger appends `change.triggered` to the stream and the Tests rows open.
- A ticketless PR runs the Review flow like any other; the policy's `require_ticket` decides whether slop fails it.
- The flows meet only in the gate's test clauses (`testset`, `tree_pinned`, `ac_covered`), which read the Tests
  flow's facts from the same view.

### B2. One table of rows, in the domain

`internal/domain/change/rows.go` replaces `workflow.Default()`, the engine's `Step` objects, `slice.go`'s trimming and
`change.rowTable()`.

```go
type Flow string // "tests", "review", "both"

type Row struct {
    ID    string
    Flow  Flow
    After []string          // row ids that must be done first
    Done  func(In) bool
    Next  func(In) []Action // called only when the flow has started, every After row is done, and Done is false
}

// In is everything a row may read. The reconciler builds it; rows never do I/O.
type In struct {
    View     View
    Policy   policy.Policy // at the PR's base SHA when there is a PR, else at the branch base (Tests flow)
    Outside  Outside       // gate facts that need I/O (B5); zero before a PR
    Profile  Profile       // the repo's promoted profile: tests dir, seam, test kinds, checks; zero when none
    Settings Settings      // from config (H5)
    Dead     []DeadJob     // outbox rows of this change that are dead or in doubt
    Now      time.Time
}

// Action is exactly one of Job, Fact, Task, Ask. Text is always set: the need in plain words.
type Action struct {
    Job  *Job
    Fact Payload // an event the server decides from In alone; the reconciler appends it
    Task *Task
    Ask  *Ask
    Text string
}
type Job  struct { Kind EffectKind; Key EffectKey; Target string; Payload any }
type Task struct { Kind string; Subject string; Input string } // Kind is a workflow kind name (B6)
type Ask  struct { Who []ActorID; What string }
```

`Missing(in In) []Need` is: for each open, not-done row in table order, the `Text` of each action of `Next` with
`Step` set to the row id; then the gate's unmet clauses (B5) with `Step: "gate"` and `Clause` set; then one need per
dead job ("<kind> failed <n> times: <last error>; `heimdall resume <change> <row>` runs it again"). No second table.

**Re-arming.** `View.Resumes map[string]int` counts `step.resumed` events per row (the existing command and event,
K7). Every job key ends with `/r<Resumes[row]>`, so `heimdall resume KAN-31 commit` makes a new key and the job runs
again. For `write_tests`, each resume also adds `Settings.TestAttempts` more attempts.

#### The rows

`v` is `in.View`; "head" is `v.PR.HeadSHA`; "submission" is `v.Tests` (the latest). `<c>` is the change id.
Tests rows need `v.Trigger != nil`; Review rows need `v.PR != nil`; when `v.Over()` no row is open.

| Row | Flow | After | Done when | Next | Key (plus `/r<n>`) |
|---|---|---|---|---|---|
| `open_room` | both | – | `v.Room != ""` | Job `room_open`; invite the test author and the assignee when there is a trigger, the PR author's mapped person when there is a PR | `<c>/room` |
| `branch` | tests | `open_room` | `v.Branch != nil` | Job `github_branch` | `<c>/branch` |
| `write_tests` | tests | `branch` | `v.Tests != nil && len(v.Tests.Rejected) == 0` | while attempts remain: Task `tests`, input `attempt-<v.Tests.Attempts+1>` (the rejection reasons are a get_task input); else Ask the operators "tests were rejected <n> times: <reasons>; `heimdall resume <c> write_tests` allows more" | |
| `test_qa` | tests | `write_tests` | `v.Tests.Verified != nil` | Job `test_quality` | `<c>/test_qa/<v.Tests.Ref>` |
| `signoff` | tests | `test_qa` | `!v.Tests.Verified.NeedsSignoff \|\| v.Tests.ApprovedBy != ""` | Ask the approvers of the sensitive domain to `heimdall approve-tests <c>` | |
| `commit` | tests | `test_qa`, `signoff` | `v.Tests.Committed != nil` | Job `github_commit_tests` | `<c>/commit/<v.Tests.Ref>` |
| `slop` | review | – | slop passed on head, or only `ticket_linked` failed (D174) | Text only: "the slop rules pass on <head> (failing: …)"; intake records slop on every head, a push fixes it | |
| `checks` | review | `slop` | `v.Checks[head]` exists, or the profile has no check whose `when` matches a changed file | Job `run_checks` (B7) | `<c>/checks/<head>` |
| `pick` | review | `slop` | `v.PR.Picked != nil` | before `v.PR.SlopAt + Settings.PickWait`: Ask the PR author "pick reviewers with `heimdall review`"; after: Fact `reviewers.picked{people: v.PR.Suggested or Settings.Reviewers, by: heimdall, method: default}` | |
| `review` | review | `pick`, `checks` | every picked seat has an outcome (review, unavailable, waived) on head | Task `review` per seat without one; subject = seat; input `head-<head>` | |
| `verify` | review | `review` | no E1 to E3 finding is unchecked | Job `verify_evidence` per unchecked finding | `<c>/verify/<ref>/<head>` |
| `deliberate` | review | `review` | no open case | Task `verdict` per open case; subject = case id; input = the case's last event id | |
| `acceptance` | review | `slop`, and only when `v.Trigger != nil` | `v.PR.CI[head]["heimdall/acceptance"] != ""` | Job `acceptance` | `<c>/acceptance/<head>` |
| `gate` | review | – | never, while the PR is open | when the decision (B5) differs from `v.PR.Gate` on head: Fact `gate.evaluated` and Job `github_check` | `<c>/gate/<head>/<first 12 hex of sha256(ok + reasons)>` |

`mutation` is not a row (Later in TECH_SPEC). Row ids stay as today; the board and the goldens use them.

**Retries are rows.** A rejected submission leaves `write_tests` open with a new input; a new head leaves `review` open
for every seat with a new input. Windows, holds, `on_fail` and writing `step.retried`/`step.escalated` are deleted.

### B3. The reconciler

`internal/app/reconcile` is the only component that starts work.

```
Run(ctx, change):                          // serialized per change: a mutex per change id inside the Reconciler
  for attempt := 1..5:
    events := Load(change); in := build In (fold, policy, outside, profile, settings, dead jobs, now)
    actions := Next of every open row
    facts := Fact actions + the task facts decided below (task.assigned, task.superseded, task.rewoken, task.failed)
    jobs  := Job actions + the task effects decided below (task_assign)
    if facts != nil: Append(change, version, facts, jobs)   // the jobs attach to the last fact
    else if jobs != nil: EnsureEffects(change, jobs)        // attached to the stream's last event
    if ErrVersionConflict: continue (reload and decide again); else return
```

- **When it runs.** (1) After every successful append to the change: `reconcile.Kicking` wraps the event store handed to
  every writer and calls `Kick(change)` once `Append` returns nil. A kick is a non-blocking send on a buffered channel
  (size 256; when full the kick is dropped, the sweep covers it) read by one worker goroutine that calls `Run`. (2) Every
  30 s, `Sweep` runs every change that is not over (`ports.OpenChanges`). Nothing else calls `Run`. The timer loop, the
  gate refresher and the nudger are deleted.
- **Jobs.** A key names its input (submission ref, head, resume count), so a new input is a new job and the same input
  runs once. Payloads hold only values from `In` (never `Now`, never a generated id), so one key always means one
  content (`EnsureEffects` errors loudly otherwise).
- **Tasks.** For each Task action, look in `v.Tasks` for a live task (status `assigned` or `acked`) of that kind and
  subject:
  - none: Fact `task.assigned{task_id, agent, role, kind, subject, input, deadline: now + timeout}` and Job
    `task_assign` keyed `<c>/task/<task id>`. The agent is the kind's holder from config (tests → test author, verdict
    → resolver) or, for review, the seat itself;
  - live on another input: Fact `task.superseded{task_id, reason: "new input <input>"}` (new event), then assign;
  - live on this input: `liveness.Next(facts, windows, now)`; the facts come from the view (`Since`, `Acked`,
    `Wakes > 0`, `StallWokenAt`) and `ports.ToolCalls` (this agent's last call while holding this task). Then: `wait` →
    nothing; `repeat_wake` → Job `task_assign` keyed `<task id>/wake/<n>` and Fact `task.rewoken{task_id, n}` (new);
    `probe_provider` → `ports.ModelHealth.Probe` and Fact `provider.probed{health}` (new); `provider_blocked` → nothing
    (`Missing` says "the model provider refuses: <health>"); `stall_rewake` → Job `task_assign` keyed `<task id>/wake/<n>`
    and Fact `task.rewoken{task_id, n, stall: true}` (D172: Switch offers an agent no session reset); `fail` → Fact `task.failed{reason: "unresponsive"}`,
    after which the row is open again and the next `Run` assigns a fresh task, at most `MaxTaskFailures` (2) times per
    input and round of the row (D173).
  - Past its deadline a live task gets `task.timed_out` (Fact), and the row reassigns.
- **Executors** perform their job and record their fact with `reconcile.Record`. They decide nothing: no log scans, no
  "am I done". A fact recorded twice (a crash after the append, before `Done`) folds to the same view (tested in H1).
- **`Record(ctx, change, build func(View) ([]Event, error)) error`** is the one way any component appends: load, fold,
  call `build`, number the events (`version+i+1`), fill `CausationID` (the last event) and `CorrelationID` (the last
  event's) when empty, append, retry up to 5 times on `ErrVersionConflict`. It replaces `engine.Advance` everywhere.
- **Intake, prsync, trigger, agent tools, operator commands** only record facts through `Record`.
- **I/O failures are needs, not silence.** If `In` cannot be built (policy, profile or outside facts unreadable), `Run`
  logs the error and returns it; the sweep retries. Rows never see an empty default in place of an unread input.

### B4. Task identity and the task index

- `task.assigned` gains `kind`, `subject`, `input` (`omitempty`, old events lack them). `bundle_ref` becomes optional
  (`omitempty`, not required by `Validate`); new tasks leave it empty.
- `tests.submitted`, `review.submitted`, `verdict.submitted` gain `task_id` (`omitempty`); every new submission sets it.
- `pr.opened` and `pr.pushed` gain `base_sha` (`omitempty`); intake sets it (it has `pr.BaseSHA`).
- View: `TaskInfo` gains `Kind`, `Subject`, `Input`, `Since`, `Wakes`, `StallWokenAt`; statuses gain `done` (a submission
  naming the task) and `superseded`.
- `postgres.TaskIndex` (finds an agent's active task) ends a task on `task.timed_out`, `task.failed`,
  `task.superseded`, or a submission whose `task_id` equals it. For events without `task_id` it keeps today's role rule,
  in one SQL branch commented `// Old events only.`

### B5. The gate, computed once

- `gate.Evaluate(gate.Facts) Decision` (`internal/domain/gate`) stays the one rule set (TECH_SPEC 12.4).
- New pure `change.GateFacts(v View, o Outside, p policy.Policy) gate.Facts` (`internal/domain/change/gate_facts.go`)
  replaces `gateuse.foldState`. `Outside` (today in `gateuse`) moves to `internal/domain/change` as plain data. The I/O
  that fills it (`gateuse.RepoSource`: the policies at base and head, the changed files, the head test tree hash, the
  policy-owner approval) becomes an adapter-facing port `ports.GateOutside` read by the reconciler.
- `Missing`'s gate part is `gate.Evaluate(GateFacts(...)).Unmet`. `gateNeeds`, `uncoveredCriteria`, `blocks`,
  `approvedOnHead` in `missing.go` are deleted.
- The `gate` row publishes when the decision changes (B2). The check payload (`CheckPayload` for `heimdall/gate`) is
  built by a pure function from the decision; `buildSummary`/`buildReasons` move to `internal/domain/gate/report.go`.
- Deleted: `gateuse.Evaluator`, `refresh.go`, `movesGate`, intake's `gateEffect`, prsync's `gate.Evaluate` calls,
  `gateRefreshLoop`.

### B6. Kinds, get_task, tools and skills

`internal/domain/workflow/kinds.go` is the one table; the `roles` field on MCP tools is deleted.

```go
type Kind struct {
    Name    string   // "tests", "review", "verdict", "profile"
    Holder  Role
    Inputs  []string // input names, rendered in this order by get_task
    Tools   []string // offered while a task of this kind is active (plus the shared tools)
    Output  string   // "TestSubmission@1", "Review@2", "Verdict@1", "Profile@1": the schema name
    Skill   string   // file under skills/, e.g. "tests.md"
    Timeout time.Duration
    Work    string   // wake line words
    Label   string   // how the board names the holder
}
```

**Shared tools** (every kind): `get_task`, `report_progress`, `propose_command`.

| Kind | Inputs (in order) | Tools | Output stored as the fact |
|---|---|---|---|
| tests | `ticket` (title, description, each criterion with id); `profile` (seam, tests dir, test kinds); `repo_map` (base); `rejection` (last reasons; attempt n of m; retries only); `answers` | `read_code`, `search_code`, `ask_humans`, `submit_tests` | `testset.Submission` |
| review | `pr` (title, body, author, base, head); `files` (changed files with +/- counts); `diff` (inline up to 40 KB, else a note); `criteria` (when a ticket is linked); `obligations` (rule cards whose `applies_to` globs match a changed file); `leads` (new check results on head, B7); `prior_findings`; `team_memory`; `repo_map` (head) | `read_code`, `search_code`, `find_symbol`, `run_check`, `ask_knowledge`, `read_card`, `stage_knowledge`, `submit_review` | `finding.Review` |
| verdict | `case` (the finding, its evidence, the challenge); `prior_decisions`; `diff` | `read_code`, `run_check`, `read_card`, `ask_knowledge`, `submit_verdict` | `deliberation.Verdict` |
| profile | `repo_map`; `stack_hints` | `read_code`, `search_code`, `propose_profile`, `trial_profile` | `Profile@1` |

- **get_task** returns one JSON object:
  `{"task": {"id", "kind", "change", "base", "head", "deadline"}, "instructions": "<skill text>",
  "inputs": [{"name", "note", "content"}], "needs": ["<Missing texts of this change>"]}`. Untrusted text (ticket, PR
  body, diff, file contents, memory) is wrapped once with the existing `wrapUntrusted`. The first `get_task` of a task
  records `task.acked` (the `ack` tool is deleted). With no active task it answers plainly and is not a refusal:
  "You have no active task. Your last task, <id> on <change>, ended: <done | superseded | failed: reason>."
- **Inputs are rendered at call time** by `internal/app/taskinput` (new): `Render(ctx, kind, view, deps) ([]Section,
  error)`, one renderer per input name in a map. It absorbs `mcp/orient.go` (ticket text, repo map),
  `mcp/brief.go` + `internal/app/reviewbrief` (obligations, prior findings, memory, record) and `agenttools/diff.go`.
  A section that fails becomes `{"name", "note": "unavailable: <reason>", "content": ""}` and the task goes on.
- **Submissions are stored whole** (A1). `tests.submitted` gains `files` ([{path, blob}]), `notes`, `task_id`,
  `base_sha`; `acceptance_criteria` and `expected_paths` stay (`ac_map`, `expected_paths` keys as today); the old
  `refs`, `sha`, `tree_hash` are no longer written (`// Old events only.`). `review.submitted` gains `task_id` and
  `lead_checks`; `verdict.submitted` gains `task_id`. The agenttools field-by-field mapping is replaced by building the
  event from the validated struct. A round-trip test per kind: a submission through the MCP tool, the fold, every
  field present in the view or the blob.
- **Tool descriptions** follow one pattern: "<what it does and what it returns>. Use it when <situation>; do not use it
  to <common misuse>. Example: <one call>." Results are compact text, not JSON dumps, except `get_task` and submit
  receipts.
- **Deleted tools:** `ack`, `get_change`, `read_untrusted`. `approve_profile` and `promote_profile` become `humanOnly`.
- **Skills:** one file per kind directly under `skills/`: `tests.md`, `review.md`, `verdict.md`, `profile.md`, each
  under 400 words with five parts: the goal (2 lines); what you already have (the input names); how to work (5 to 8
  bullets: order of work, when to read code, when to ask a person, when to stop); what is accepted (the rules the
  server enforces on submission, so a rejection never surprises the agent); how to finish (the submit tool and its
  required fields). A skill never tells the agent to fetch an input it has and never lists tools. The directories
  `skills/test-quality`, `test-method`, `reviewer`, `review-method`, `resolver` are deleted after their acceptance
  rules are carried over; `skills/embed.go` returns the file named by `kind.Skill` (no role switch).

### B7. Checks: leads, `run_check`, E2 evidence

A check is a row of the repo's promoted profile (`ports.ProfileCheck`: id, lens, when, image, run, baseline, history).
The server names no tool and no language.

- **`run_checks` job** (new effect kind, executor `internal/app/effects/checks.go`): payload `{repo, change, head, base,
  check_ids}` (the ids whose `when` matches a changed file). Per check: `SandboxJob{Kind: SandboxCheck, CheckID, Repo,
  SHA: head, Change, Timeout}`; with `baseline`, also `SHA: base, Head: head`; `sarif.Parse` each `Sarif`;
  `sarif.New(base, head)`. Record one `checks.reported{head, results: [sarif.Result...], errors: [{check, reason}],
  more: n}` (new event; at most 200 results kept, the rest counted in `more`). A check that cannot run is an `errors`
  entry, not a job failure.
- View: `v.Checks map[id.SHA]CheckInfo{Results, Errors, Ran}`.
- **`run_check(check_id)`**: runs one profile check on head (`OnDemand: true`, baseline as the profile says) and returns
  the new results as lines `file:line rule: message`. The description says the id is one of those in the `leads`
  input; an unknown id is refused with the list. Not offered when the repo has no promoted profile with checks. At most
  5 calls per task (`maxChecksPerTask`, kept).
- **E2 evidence:** `finding.ToolResult` (today `tool`, `args`, `location`) gains `check_id` (a profile check id;
  required for new findings, `omitempty` for old ones); the submit rules refuse an E2 finding whose `check_id` is not a
  check of the profile.
  `verify_evidence` re-runs that check on head: a new result inside the finding's file and line range confirms E2;
  otherwise the finding is E4.
- Deleted: `ports.SandboxStatic`, `ports.CheckGoVet`, `SandboxJob.Check`, `verify.ParseVet`, the runner's static branch
  (`internal/adapters/rootlessrunner/api.go`, the `job.Kind == ports.SandboxStatic` block) and their tests.

### B8. No wiring bugs

- **Table check** `workflow.CheckKinds(tools, renderers, schemas, skills)`, run by a unit test and at server start: every
  kind's tools are registered MCP tools; every input name has a renderer; every output schema name is registered; every
  skill file exists, is under 400 words and names no tool the kind does not hold. Plus: every row's job kind has an
  executor. A failure stops the server and lists every problem.
- **Boot self-check** (`cmd/heimdall-server/selfcheck.go`): call each tool dependency once with a 10 s timeout — the
  runner (health and accepted job kinds), the code host (read one file at the default branch of each mapped repo),
  knowledge search (health). A tool whose dependency failed is not offered; one log line per withheld tool with the
  reason; `/healthz` reports `{"degraded": ["run_check: runner unreachable"]}`.
- **Contract tests:** the runner client against the runner's own HTTP handler (both in this repo) for `check` and `test`
  jobs.

### B9. End-to-end checks of the product

1. **Tests flow** (`cmd/heimdall-server/tests_flow_component_test.go`, in `make verify`): the real app wiring and
   Postgres, fake Jira and GitHub (`fakes_component_test.go`), a fake runner; a scripted test author over MCP HTTP:
   trigger → room → branch → `get_task` → a submission the verifier rejects → `get_task` shows the rejection → a good
   resubmission → verified → committed → a PR head gets `heimdall/acceptance` queued.
2. **Review flow** (`review_flow_component_test.go`): a ticketless PR on a repo whose profile has one check; checks
   report a lead; the pick falls back after the wait (fake clock); three scripted seats (one files an E2 finding citing
   the check); verify confirms it; a push supersedes the three tasks and they re-review; one seat goes silent, is
   rewoken, reset, failed and reassigned; an approval from a non-author approver; the gate check is green on head.
3. **Full, live** (H11): a real Jira ticket, the real agents, a real PR on `aarush323/heimdall-fixture-pay` (base
   `heimdall-test/demo-base`), approval by `rekdfr`, merge; one ticketless PR reviewed.

`e2e/stories` is retired (owner, Nov 17).

---

## Part C. Build steps

Before H1 run `make fast` and record the test count in PROGRESS. Every step ends with `make fast` and `make verify`
green (except the known `secrets` false positive listed in PROGRESS, which needs the owner) and a commit.

### H0. Ground

- **Change:** `docs/PROGRESS.md`: new top section "Harness overhaul (D170)" listing H0 to H12 as pending, with a link
  to this file. One line at the top of `docs/BUILD_PLAN.md` and `docs/AGENT_INTERACTION_ARCH.md`: "Superseded where it
  conflicts by docs/HARNESS_DESIGN.md (D170)."
- **Done when:** committed `H0: adopt the harness design (D170)` (together with `HARNESS_DESIGN.md`,
  `HARNESS_ROOT_CAUSES.md`, `LIVE_RUN_KAN24.md` if still untracked).

### H1. View fields and new events

- **Goal:** every question a row, an executor or the gate asks is answered by a field of `View`.
- **Read first:** `internal/domain/change/view.go`, `events_*.go`, `payload.go`, `testdata/plan/view/README.md`,
  `internal/app/gateuse/inputs.go`, `internal/app/engine/steps_fanout.go`, `steps_resolver.go`.
- **Change (`internal/domain/change`):**
  - `TestsInfo` + `Ref workflow.BlobRef` (`e.BlobRefs[0]` of `tests.submitted`), `Attempts int` (count of
    `tests.submitted`, never reset), `TaskID`, `BaseSHA`.
  - `TaskInfo` + `Kind`, `Subject`, `Input`, `Since` (`OccurredAt` of `task.assigned`), `Wakes`, `StallWokenAt`; statuses
    `done`, `superseded`. A submission with `task_id` sets that task `done`.
  - `ReviewInfo` + `Outcome` (`submitted`, `unavailable`, `waived`); fold `review.unavailable` and `review.waived`
    (not folded today).
  - `PRInfo` + `Base` (from `base_sha`), `SlopAt` (`OccurredAt` of the slop event on the current head), `Suggested`.
  - `View.Cases map[id.CaseID]CaseInfo{Finding string, Open bool, LastEvent EventID}`; `View.Resumes
    map[string]int`; `View.Checks` (H8 fills it; add the type now).
  - What `gateuse.foldState` collects that `View` lacks (review requests known, human-pending cases, finding states
    with what consensus needs). List each in the commit message.
  - Payload fields from B4 and B6 (`omitempty`); new event types `task.superseded`, `task.rewoken`, `provider.probed`,
    `checks.reported` (payload, `Validate`, registry entry, fold).
- **Tests (`view_test.go`, first):** a table test per new field (events in, field out).
  `TestFoldIsIdempotentForExecutorFacts`: for `room.opened`, `branch.ensured`, `tests.verified`, `tests.rejected`,
  `tests.committed`, `finding.verified`, `ci.reported`, `checks.reported`, `gate.evaluated`: `Fold(evs)` equals
  `Fold(append(evs, dup))`. `TestOldPayloadsDecode`: each changed payload as stored today still decodes.
- **Done when:** `make fast` green; the `testdata/plan/view` goldens reproduce, or differ only by new fields —
  regenerate with the existing generator and list the diff in the commit (M0 phase 2 approval).
- **Traps:** `Decode` is strict: add, never rename. `review.submitted` folds `HeadSHA: v.head()`; keep that.

### H2. The row table and `Missing`

- **Goal:** `change/rows.go` per B2; `Missing` derived from it.
- **Create:** `internal/domain/change/rows.go` (types `Flow`, `Row`, `In`, `Settings`, `Profile`, `DeadJob`, `Action`,
  `Job`, `Task`, `Ask`; `Rows() []Row`; `Next(in In) []Action`), `rows_tests.go`, `rows_review.go`, `rows_test.go`.
  `Settings{Reviewers []ActorID; TestAttempts int; PickWait time.Duration; Timeouts map[string]time.Duration}`.
- **Change:** `missing.go`: `Missing(in In) []Need` from `Next`, gate clauses, dead jobs; keep today's `gateNeeds` as
  the gate part in this step (H6 replaces it). Update every caller of `Missing(v, p)`.
- **Job payloads** are the existing effect payload types (`RoomOpenPayload`, `BranchPayload`, `TestQualityPayload`,
  `CommitTestsPayload`, `VerifyEvidencePayload`, `CheckPayload`); for each, read its `Decode*Payload` and the engine
  step that filled it (`engine/steps_*.go`) and fill the same fields from `In`. `TaskAssignPayload.Bundle` becomes
  `omitempty` (inputs are rendered at call time from H7; until H7 the reconciler still builds the bundle — see H4).
- **Tests (first):** per row: closed before its flow starts; open after its `After`; the done rule; `Next` (kind, key,
  payload) on a fixture view. The live bugs as cases:
  - B4: submitted → rejected → resubmitted: `test_qa`'s key names the **second** submission ref.
  - K5: a ticketless view with only `pr.opened` + passing slop: `open_room`, `checks`, `pick` and `gate` are open;
    `acceptance` is not (no trigger); no Tests row is open.
  - pick: before the wait → Ask; after → Fact with `Suggested`; no suggestions → `Settings.Reviewers`.
  - review: a new head → a Task per seat with input `head-<new>`.
  - budget: `Attempts == TestAttempts` and rejected → Ask, not Task; one `step.resumed{write_tests}` → Task again.
  - re-arm: `Resumes["commit"] = 1` → key ends `/r1`.
  - determinism: `Next` twice on one `In` gives equal actions (`reflect.DeepEqual`); no Job payload contains `In.Now`.
- **Done when:** `make fast` green; the `Missing` goldens in `testdata/plan/view` reproduce, or differ only where B1
  changes behaviour (no `wait_pr` need; Review needs before commit) — each listed in the commit.
- **Traps:** `In.Now` only (lint + R5). Row ids unchanged.

### H3. Store support

- **Goal:** the ports the reconciler needs, with Postgres adapters.
- **Create in `internal/ports`:**
  - `EffectEnsurer.EnsureEffects(ctx, change id.ChangeID, fx []change.Effect) error`: each effect inserted attached to
    the change's last event; same key + same content: no-op; same key + other content: error.
  - `OpenChanges.OpenChanges(ctx) ([]id.ChangeID, error)`: changes the fold would not call `Over()` — no `pr.closed`
    after the last `pr.opened`, no `change.rejected`, no `decision.recorded` with kind `reject`. A component test
    compares it with `Fold(...).Over()` on fixture streams.
  - `DeadEffects.DeadEffects(ctx, change id.ChangeID) ([]change.DeadJob, error)`: outbox rows joined to `event` on
    `event_id` with that `change_id`, `done_at IS NULL` and (`attempts >= 8` or (`started_at IS NOT NULL AND
    claimed_until < now()`)); returns key, kind, attempts, last error, in-doubt flag.
  - `ModelHealth.Probe(ctx) (liveness.Health, error)` (after H4 moves liveness into the domain).
- **Create:** `internal/adapters/postgres/ensure.go` (reuse `insertEffect` inside a transaction), `open_changes.go`,
  `dead_effects.go`, with component tests in the existing `*_component_test.go` style;
  `internal/adapters/openrouter`: a `Probe` that sends one minimal request with the server's own key (read the adapter
  for how it calls OpenRouter today) and maps 401/402/429/5xx to `refusing`.
- **Done when:** the component tests pass in `make verify`.
- **Traps:** the outbox guard trigger (migration 0002) refuses most updates; inserts are fine.

### H4. The reconciler

- **Goal:** `internal/app/reconcile` per B3, tested on fakes; not wired yet.
- **First:** move `internal/app/liveness` → `internal/domain/liveness` and `internal/app/questions` →
  `internal/domain/questions` (both are pure; archcheck forbids app → app imports). Update imports.
- **Create:** `reconcile.go` (`Reconciler`, `Config{Store ports.EventStore; Effects ports.EffectEnsurer; Open
  ports.OpenChanges; Dead ports.DeadEffects; Inputs InputSource; Calls ports.ToolCalls; Health ports.ModelHealth;
  Agents func(kind, subject string) (id.AgentName, bool); IDs ports.IDs; Clock ports.Clock; Settings change.Settings;
  Windows liveness.Windows; Logger *slog.Logger}`, `New`, `Run`, `Sweep`, `Loop(ctx)` that reads kicks and ticks
  every 30 s via `clock.Wait`), `record.go` (`Record`), `kicking.go`, `tasks.go`, `inputs.go` (`InputSource`:
  `Policy(ctx, repo, sha)`, `Outside(ctx, view)`, `Profile(ctx, repo)`; results cached per (repo, sha) for 60 s using
  `Clock`), `doc.go` (from `docs/harness/doc.go.template`), tests.
- **Bundle until H7:** `task_assign` still needs today's bundle bytes; `InputSource` gets `Bundle(kind, view)` backed
  by `internal/app/bundle` through a small port, deleted in H7.
- **Tests (fakes, first):** `change.triggered` only → `room_open` ensured; a second `Run` → nothing new; after
  `room.opened` → `github_branch`; a version conflict mid-`Run` → retried, one result; a rejected submission → a new
  `tests` task with input `attempt-2`; a new head with three live reviews → three `task.superseded` + three
  `task.assigned`; ack window passed → wake effect `<task>/wake/1` + `task.rewoken`; stall after reset →
  `task.failed`, and the next `Run` assigns a new task; past the deadline → `task.timed_out`; a dead job → in `Missing`,
  nothing re-inserted; `heimdall resume` (a `step.resumed`) → a new key; `Sweep` runs each open change once; `Kicking`
  kicks after a successful `Append` and not after a failed one; two concurrent `Run`s of one change serialize.
- **Done when:** `make fast` green, archcheck passes.

### H5. Switch over; delete the engine

- **Goal:** the reconciler runs the product; engine, timers, nudger gone.
- **Change (`cmd/heimdall-server`):**
  - `app.go`: build the `Reconciler`; wrap `postgres.Store` in `reconcile.Kicking` and pass the wrapped store to every
    writer; start `rec.Loop` from `startLoops`; remove `engine.New`, `timers`, `nudger`.
  - Every `eng.Advance(...)` (today: `bridge.go` ingestor and starter, `wire_core.go` twice, `operator_reject.go`,
    `operator_answer.go`, `operator_recheck.go`, `command_consumer.go` twice) becomes `reconcile.Record`; the executors'
    `effects.Ingest` port becomes `Record`.
  - `resume.go`: keep the command; `step.resumed` is now the re-arm and budget event (B2); delete its windowing logic.
  - Trigger: `starter.Start` records `change.triggered` through `Record` when the view has no trigger; with one, it
    returns an error wrapping `ports.ErrVersionConflict` (trigger's dedupe contract, `internal/app/trigger/doc.go`).
  - `Settings`: `Reviewers` = `config.Agents.Reviewers`; `TestAttempts` = today's `maxAttempts`; `PickWait` 1 min;
    timeouts from the kind table (tests 2 h, review 30 min, verdict 30 min); liveness windows ack 3 min, stall 10 min.
  - `Agents`: tests → `config.Agents.TestAuthor`; verdict → `config.Agents.Resolver`; review → the seat name when it
    parses with `id.ParseAgentName`.
- **Delete:** `internal/app/engine/` (all), `cmd/heimdall-server/slice.go`, `timers.go`, `nudge.go`, `nudge_test.go`,
  `jitter.go` if nothing else uses it; `internal/domain/workflow/definition.go`, `rows_core.go`, `rows_tests.go`,
  `rows_review.go`, `validate.go`, their tests and `internal/domain/workflow/testdata` (keep `kinds.go`, `task.go`,
  `Role`, `BlobRef` and whatever else still compiles in use).
- **Port tests:** the component tests that drove the engine (`gate_component_test.go`, `review_gate_journey_test.go`,
  `live_gate_*_test.go`, `bridge_test.go`, `mcp_test.go`, `acceptance_component_test.go`,
  `resume_consumer_test.go`) drive `Record` + `Run`; keep their assertions; where one asserted `step.retried` or a
  timer, assert the row behaviour instead and say so in the commit.
- **Also:** `scripts/check-owned.sh` rows naming engine paths: remove those globs.
- **Done when:** `make verify` green; `grep -rn "internal/app/engine\|workflow.Default\|\.Advance(" --include=*.go .`
  finds nothing.
- **Traps:** the engine's callers also captured workflow memory inline (`ingestor`, `starter.capture`); the durable
  `workflowMemoryLoop` backfills from the global order, so drop the inline capture and check that loop's test covers
  it. `task_assign` posts in `v.Room`; read `effects/task_assign.go` for the room fallback it uses.

### H6. Executors, intake, prsync, the gate

- **Goal:** executors and intake only record facts; the gate is computed once (B5); PR-first changes (B1).
- **Executors (`internal/app/effects`):** delete `loadedChange` and its methods (`has`, `first`, `last`,
  `judgedSince`), `findCauseEvent`, `wasVerified`, every "already done" check. Each executor reads its payload, does
  the job, records its fact. A value an executor used to scan for goes into its payload (H2).
- **Intake:** delete `ErrNoChange` (a PR naming an unknown ticket records `pr.opened` as version 1 of the ticket's
  stream), `gateEffect`, `acceptanceEffect` and their constants; set `base_sha`; keep `checkSecondPR`, slop judging and
  `expectedPaths`.
- **Prsync:** delete the `gate.Evaluate` calls and its `GateEvaluate` port; it records approvals, CI and closes.
- **Gate, in this order:**
  1. Move `Outside` to `internal/domain/change`; write `change.GateFacts`.
  2. Write `internal/app/gateuse/equivalence_test.go`: for every event sequence used by the gateuse tests
     (`gateuse_test.go`, `inputs_test.go`, `consensus_internal_test.go`, `verified_consensus_test.go`,
     `lead_review_test.go`, `severity_test.go`), `gate.Evaluate(GateFacts(Fold(evs), o, p))` equals today's decision.
     Make it pass by fixing `GateFacts` (never `gate.Evaluate`).
  3. Switch `Missing`'s gate part; add the `gate` row's Fact + Job; move `buildSummary`/`buildReasons` to
     `internal/domain/gate/report.go`; `ports.GateOutside` with the adapter built from today's `RepoSource`.
  4. Delete `gateuse/inputs.go`, the `Evaluator`, `refresh.go` and their tests; keep the equivalence cases as
     `internal/domain/change/gate_facts_test.go`. Delete `gateRefreshLoop` and its wiring.
- **Tests:** intake: a PR for an unknown ticket starts the change; the B2 case (slop scope passes with the expected
  paths). Each executor test asserts only "payload in → port calls + recorded fact".
- **Done when:** `make verify` green; `grep -rn "loadedChange\|movesGate\|ErrNoChange\|gateRefresh"` finds nothing.

### H7. Kinds, get_task, tools, skills, submissions

- **Goal:** B6, all of it.
- **Order:**
  1. `workflow/kinds.go`: the new fields and rows; `CheckKinds` (B8).
  2. `internal/app/taskinput`: `Section{Name, Note, Content}`; one renderer per input name (B6). Move the code from
     `mcp/orient.go` (ticket text, repo map), `mcp/brief.go` and `internal/app/reviewbrief` (obligations, prior
     findings, memory, record), `agenttools/diff.go` (diff). Obligations: only cards whose `applies_to` globs match a
     changed file (B11). Diff: inline up to 40 KB, else note "The diff is <n> KB; read the files you need with
     read_code."
  3. `internal/transport/mcp`: `get_task` per B6 (acks on first call; plain answer with no task); delete `ack`,
     `get_change`, `read_untrusted`, the `roles` field and the bundle use; offered tools = shared + `kind.Tools`;
     `approve_profile`, `promote_profile` → `humanOnly`.
  4. Submissions stored whole with `task_id` (B6); the agenttools mapping builds events from the validated structs.
  5. Every tool description rewritten to the B6 pattern. Write `skills/tests.md`, `review.md`, `verdict.md`,
     `profile.md`; delete the old skill directories (B6); `skills/embed.go` reads `kind.Skill`.
  6. Delete `internal/app/bundle`, the reconciler's bundle port (H4), and `TaskEnvelope.Inputs`' reference fields once
     unused.
  7. Update `cmd/heimdall-testagent` to the new tools.
- **Tests:** `TestGetTaskCarriesEveryInput` per kind (fixture view → each input present, with content or an
  "unavailable" note); `CheckKinds` passes on the real table and fails on a missing tool, a missing renderer, a skill
  naming a foreign tool; the round trip per kind; the MCP tool-list test lists exactly the tools per kind (replaces the
  per-role lists; R2 note in the commit).
- **Done when:** `make verify` green; `wc -w skills/*.md` each under 400;
  `grep -rn "read_untrusted\|get_change\|\"ack\"" internal cmd skills` finds nothing but old-event comments.
- **Traps:** agents in Switch hold a cached tool list; `agentSyncLoop` refreshes agent definitions at start — check
  that it also refreshes their MCP tool list, or the H11 deploy needs an agent restart (record which).

### H8. Checks

- **Goal:** B7.
- **Create:** `change.EffectRunChecks`, its payload and `Decode`; fill the `checks` row and `v.Checks` fold;
  `internal/app/effects/checks.go` + tests (a fake sandbox returning SARIF from `testdata/plan/sarif`); the `leads`
  renderer; `run_check` rewritten; `verify_evidence` E2 on profile checks; `finding.ToolResult.CheckID` and its submit rule.
- **Delete:** `ports.SandboxStatic`, `ports.CheckGoVet`, `SandboxJob.Check`, `verify.ParseVet` and tests, the runner's
  static branch and tests, the go-vet text in `formatCheck`.
- **Tests:** executor: baseline + head → only new results; a failing check → an `errors` entry and the job done;
  `run_check`: unknown id refused with the list; the 5-call budget; E2: a located new result confirms, none → E4.
- **Done when:** `make verify` green; `grep -rn "SandboxStatic\|CheckGoVet\|ParseVet"` finds nothing.
- **Traps:** a baseline job is `SHA: base, Head: head` (`SandboxJob.Head` doc). Results are capped by `MaxJobResult`.
  Confirm `aarush323/heimdall-fixture-pay` has a promoted profile with at least one check before H11; if not, propose
  one through `propose_profile` and `trial_profile` and have the owner approve and promote it.

### H9. Wiring proofs

- **Goal:** B8.
- **Create:** `cmd/heimdall-server/selfcheck.go` + test (a fake failing one dependency → that tool withheld, one log
  line, `/healthz` degraded); `CheckKinds` and "every row job kind has an executor" at start-up; the runner contract
  test (`internal/adapters/rootlessrunner` client against its handler for `check` and `test`).
- **Done when:** `make verify` green; starting the server with the runner socket missing logs
  `run_check withheld: runner unreachable` and a reviewer's tool list lacks `run_check`.

### H10. Component end-to-end checks; retire stories

- **Goal:** B9 items 1 and 2 in `make verify`.
- **Create:** `cmd/heimdall-server/tests_flow_component_test.go`, `review_flow_component_test.go`: the real wiring
  (`assemble` with the fakes in `fakes_component_test.go`), a scripted agent over the MCP HTTP handler (reuse
  `cmd/heimdall-testagent` pieces), a fake clock, `rec.Sweep` called instead of waiting.
- **Delete:** `e2e/stories/`, its Makefile target and references in docs (owner approval quoted in Part A).
- **Done when:** both pass in `make verify`, each under 20 s.

### H11. Deploy and live run

- **Before:** state the expected cost (DeepSeek v4 flash via OpenRouter, about $0.30 to $1 per full run; cap $3).
  Close the old fixture PRs #13 (KAN-24) and #14 (KAN-25) with a comment, so no open change has old shapes.
- **Deploy:** create `scripts/ops/hot-deploy.sh` (no secrets): `pg_dump` backup to `/opt/heimdall/backups`, keep
  `.prev` copies of `/opt/heimdall/heimdall-server` and `/usr/local/bin/heimdall-runner`, `install -m 0755` the new
  ones from `/tmp`, `systemctl restart heimdall-runner heimdall-compose`, print the binary hashes inside the container,
  `curl -sS http://127.0.0.1:8080/healthz`. Build both binaries per `docs/RUNBOOK.md`, copy to the VM (`ssh
  heimdall-vm`), run the script with root via the owner's Terminal panel. Rollback: copy `.prev` back, restart.
- **Run:** create a KAN ticket with 2 to 3 acceptance criteria (`.cache/live/jira.sh create`, which reads `.env`
  without printing it), assign, move to In Progress; watch with `.cache/live/events.sh <KEY>`. Expect room, branch,
  tests task, verified, committed. Then implement the change on the ticket branch of the fixture repo and open a PR to
  `heimdall-test/demo-base`; watch checks, pick, three reviews, verify, gate; approve as `rekdfr` (pass that login's
  token explicitly to `gh`); merge when the gate is green. Open one ticketless PR and see it reviewed.
- **Record:** `docs/LIVE_RUN_H11.md`: timeline, minutes per row, tool calls and refusals per task (from `tool_call`),
  cost, every defect. A defect is fixed in code with a failing test first and that part re-run; never patched on the VM.
- **After:** stop the VM: `aws ec2 stop-instances --profile heimdall --region ap-south-1 --instance-ids
  i-019af6359c549dfb0`.

### H12. Specs and leftovers

- Rewrite TECH_SPEC 5 (workflow engine → rows and reconciler), 6.2 (task index), 12.4 (gate path); update
  `docs/RUNBOOK.md` (no timer or nudge loops; the sweep; dead jobs and `heimdall resume`) and every `doc.go` touched.
- Then, one commit each:
  - **A6, B7, B10 policy versions:** `version: 2` is the seats shape; one tested converter from v1; a policy that does
    not decode is a need on every change it affects (`Missing`), never a silent warning.
  - **B9 redaction:** `internal/app/record` must not redact file paths (a long run containing `/` or `.` is a path).
- **Later (needs the Switch `update_message` patch, owner):** A7, one status message per change rendered from
  `Missing`.

---

## Part D. Risks

| Risk | Handling |
|---|---|
| Two writers race on one change | every append is at an expected version; conflicts reload and decide again; `Run` is serialized per change |
| One key, two payloads | payloads are built only from `In`; the H2 determinism test; `EnsureEffects` errors loudly |
| A lost kick | the 30 s sweep is the guarantee; kicks only make it fast |
| Old streams with `step.*` events and old payloads | they decode; the fold ignores what is old; PRs #13 and #14 closed before H11 |
| A job fails forever or is in doubt | it is a need with its last error and the `heimdall resume` line; nothing loops on it |
| The gate refactor changes a decision | the H6 equivalence test runs every existing gate case through both paths before the old one is deleted |
| An agent's cached tool list is stale after deploy | H7 trap: confirm the agent sync refreshes tools; H11 checks each agent's list once |
| An unreadable policy or profile hides a feature | `Run` fails loudly for that change and the sweep retries; no empty defaults |
