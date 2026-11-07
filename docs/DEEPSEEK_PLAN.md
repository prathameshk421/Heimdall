# Heimdall execution notes for DeepSeek (historical)

Plan version 2, Nov 4, 2026. This replaces the earlier draft of this file.

**What this file is.** A dated execution plan and record of the Nov 4 queue. It is not the current work order or a rulebook. Use `docs/PROGRESS.md` and the task's current implementation-plan acceptance criteria to choose useful work. The board and external prerequisites are evidence, not gates on independent local work.

**What this file is not.** It does not outrank `AGENTS.md`, `TECH_SPEC.md`, `MVP_SCENARIOS.md`,
`TEST_STRATEGY.md` or `CODING_STANDARDS.md`. If this plan disagrees with any of them, they win, and the plan
is corrected in the same commit that notices the disagreement. **`AGENTS.md` is the contract every session
signs by working here.** Read it first, every session, even if you remember it.

**Owner instruction for this stretch.** Finish the half-done work first (that is W0). Then take the
low-brain, easily verifiable features in dependency order. Keep the server/VM/agent-runtime wiring for later.
No shortcuts, no fallbacks, no cheat codes: this is the real product, not a demo that looks green.

---

## 0. How to use this file

### 0.1 Read order (do not skip, do not reorder)

| # | File | What you take from it |
|---|---|---|
| 1 | `docs/PROGRESS.md` | where the build actually is, what failed last session, spend, open blockers |
| 2 | `AGENTS.md` | the binding rules R1–R13, the commands, the branch, the things you cannot infer from code |
| 3 | `docs/MVP_SCENARIOS.md` section 7 | the one build order (D156, D159) |
| 4 | `docs/IMPLEMENTATION_PLAN.md` — the row of the task you are doing | its done-when, dependencies, spec sections |
| 5 | `docs/TEST_STRATEGY.md` sections 2, 3, 5 | the layers, the allowed doubles, the per-task loop and definition of done |
| 6 | `docs/CODING_STANDARDS.md` | structure, errors, purity, tests, dependencies, sections 8 and 12 especially |
| 7 | the spec sections the task row cites in `docs/TECH_SPEC.md` | the behaviour |
| 8 | the `doc.go` of every package you touch | its design note and traps; if it has none, you write it first |
| 9 | optional historical notes in section 3 | context only; current progress and task acceptance criteria take precedence |

If a package has its own `AGENTS.md` beside its `doc.go`, read that too; it is the local trap list.

### 0.2 Authority when documents disagree (D138)

```
highest-numbered backlog part from Part 8 on
  > docs/TECH_SPEC.md
  > earlier backlog parts
  > this plan
```

`MVP_SCENARIOS.md` and `TEST_STRATEGY.md` govern testing. `CODING_STANDARDS.md` governs code. `AGENTS.md`
governs the session. A real result that contradicts a decision changes the decision **and** the spec in the
same commit; it never gets a quiet fallback.

### 0.3 Environment snapshot (optional baseline)

```bash
cd /home/aarush_linux/projects/switch_shi
git status --short && git log --oneline -3
df -h /            # keep at least 5 GB free; prune only regenerable caches
export PATH=$HOME/.local/go/bin:$PATH
make fast          # record the red set before touching anything
```

Read section 0.1 as useful context. Record baseline failures in `docs/PROGRESS.md`, keep changes focused, and use task-scoped checks to make progress when unrelated failures prevent the full suite from running. Do not hide or misattribute baseline failures.

### 0.4 Handoff notes

- Record the change, checks and failures accurately; do not claim a failing or unrun check passed.
- `docs/PROGRESS.md` has the evidence bullet from section 1.4, including **not verified** items.
- The task's `docs/TRACKER.md` row is `done` only if its done-when is met; else `doing` with what is missing.
- `git status` has no stray binary, no `out.xml`, no scratch file, no `.env` anything.
- Use focused, reviewable commits when committing; the message does not claim a check that did not run.

---

## 1. The contract, made checkable

### 1.1 Rules R1–R13 and where they are enforced

`AGENTS.md` is the source. This table is the operational form; it does not replace it.

| Rule | What the executor must do | What catches a violation |
|---|---|---|
| R1 red first | write the test, run it, see an **assertion** failure, keep the output. A compile error is not a red. | review of the saved output; mutation check |
| R2 never weaken | no skipped/deleted/loosened test, no `//nolint`, no edited `.golangci.yml`, `e2e/`, `testdata/real`, `Makefile`, harness without the owner's yes | `make lint-selftest`, hooks, review, `make verify` |
| R3 dependency rule | `domain` → stdlib + domain; `app` → domain + ports; adapters → ports; transport → app; nothing production → `internal/testkit` | `internal/archcheck`, depguard |
| R4 no test code in production | fakes in `_test.go` or `internal/testkit`; no test-mode flag, env switch or special branch | depguard, archcheck, review |
| R5 pure domain | no I/O, clock, randomness or logging in `internal/domain`; time and ids through ports | lint (`forbidigo`, globals), review |
| R6 errors | wrap with `%w` and context; never swallow; `errors.Is/As` for branching; no `panic` outside `main`/tests | `errcheck`, `errorlint`, review |
| R7 hygiene | no `time.Sleep`, `fmt.Print*`, `os.Getenv` outside `internal/platform/config`, package globals, doing `init()` | lint |
| R8 secrets | never read/print/log/commit `.env`, `secrets/`, tokens, keys; live tests read them inside the Go process | hook, gitleaks, review |
| R9 real or not verified | if it did not run on the real system, write `not verified` and why; never "should work" | evidence review |
| R10 scope | one task, one concern; unrelated findings go to `PROGRESS.md` "Noticed" | diff review |
| R11 git | no `--no-verify`, no force-push, no history rewrite, no push unless asked, no red commit | hook, review |
| R12 stuck | after two failed attempts at the same error, write what you tried in `PROGRESS.md` and stop repeating that approach; continue independent work | session review |
| R13 no cuts/hacks | do not drop or reduce a task, step or check; no temporary shortcut; record blockers and continue independent work; ask only when an owner decision/access change is needed | this plan, review, gate |

A rule broken twice becomes a lint rule or a hook, not a paragraph of advice (`CODING_STANDARDS.md` 13).

### 1.2 Evidence checklist (apply the relevant items)

Derived from `TEST_STRATEGY.md` section 5 and made checkable:

| # | Gate | Evidence required |
|---|---|---|
| 1 | Board status, when relevant | `make prototype ONLY=<slice>` result, or the reason the environment is unavailable |
| 2 | `doc.go` written or updated **before** code | file in diff, Purpose/API/Invariants/Failure modes/Tests |
| 3 | Red seen | test name + the assertion line, saved in `PROGRESS.md` |
| 4 | Green | `go test -count=1 ./<pkg>/...` exit 0 |
| 5 | `make fast` | exit 0, test count and `0 issues` in the log |
| 6 | `make verify` | exit 0, or the failure and affected checks recorded |
| 7 | Real check, or `not verified` | command and result, or reason unavailable; required acceptance stays open until verified |
| 8 | Mutation check | 4–5 one-line flips, each turned a test red; survivors recorded as missing tests |

**Not done if any of these is true:** a test was written after the code and never seen red; a test passes
because the implementation returns the fixture's value; a lint rule was disabled; a check was skipped; the
only proof is a mock when the done-when names a real system; the evidence says "should work"; a TODO marks
the missing half.

### 1.3 The no-shortcut catalog (what "actual product" means here)

Anything in this table is a cheat code. It is rejected at review even if every command is green.

| # | Shortcut | Why it is refused | Do this instead | Caught by |
|---|---|---|---|---|
| 1 | Stub returns `nil`/success so callers compile | the product does nothing; tests prove nothing | return `not implemented` until the real path exists | red test must fail on the assertion, not compile |
| 2 | Hardcoded fixture answer (`if key == "KAN-1"`, canned room id) | passes only the fixture, breaks on the real system | parse the real payload; fixtures are inputs, not answers | mutation + live/contract check |
| 3 | Test-mode branch (`if testing.Testing()`, env switch, build tag in production) | test code in production (R4/D121) | inject the dependency through a port | archcheck + review |
| 4 | Catch-all fallback (`if err != nil { return default, nil }`) | hides a real failure; the caller cannot act | return the wrapped error; a documented retry is fine, a second path is not | `errcheck` + review |
| 5 | Swallowed error (`_ =`, empty `if err != nil {}`) | data loss with a green build | wrap and return, or log **and** return | lint + review |
| 6 | `time.Sleep`/poll loop to fake async | flaky, hides a race | wait on a channel/condition with a deadline; fake the clock | lint + `-race` |
| 7 | Assertion-free test (`if err != nil { t.Fatal }`) | proves only that code ran | assert the value, the call, the emitted event | mutation check |
| 8 | Golden generated by the code under test and accepted blindly | the test agrees with the bug | hand-write the expected value from the spec/fixture source | review |
| 9 | Deleted/renamed/skipped failing test | R2 | fix the code or ask; the test defines "passing" | `make lint-selftest`, unit target fails on skip |
| 10 | `//nolint` without a cited, owner-approved reason | silences the cheapest check | fix the cause, or stop and ask | lint config |
| 11 | Adding an exclude path to `.golangci.yml` | same, one level up | fix the package | guarded file, hook asks |
| 12 | `t.Skip` when a credential is absent **and no blocker written** | looks green, proves nothing | skip **and** write `not verified` with the reason in PROGRESS/TRACKER | evidence review |
| 13 | Mock-only proof for a real-system done-when | the contract is unverified | run the live/contract check or record the blocker | gate 7 |
| 14 | A second code path "just in case" | speculative fallback (D122) | delete the loser of a spike; one path | review + diff |
| 15 | TODO/FIXME in shipped code for the task's own scope | the task is not finished | finish it, or split the task with the owner's yes | review |
| 16 | Silent truncation where the spec says refuse | data loss presented as success | return the cap error with the numbers | test at the boundary |
| 17 | Broad `recover()` to keep going | hides a panic, loses the cause | return an error; panic only in `main`/tests | review |
| 18 | Package-level mutable state or `init()` that works | R7, untestable ordering | pass the dependency in | lint |

**The honest implementation test.** Delete every `_test.go` file and every fixture. Would the package still
do the real thing for a real caller? If no, it is a demo, not product code. (This is a thought experiment,
not an instruction to delete tests.)

**The mutation test.** Break the code one line at a time. If the suite stays green, the test does not test
that line. Add the missing test; do not weaken the mutation.

### 1.4 Evidence format (copy this)

One bullet per task, under the current session in `docs/PROGRESS.md`:

```
- **T## done:** <one sentence>. Built: <packages/files>. Evidence: red <test> failed on `<assertion>`;
  `go test -count=1 ./<pkg>/...` exit 0; `make fast` exit 0 (<n> tests, lint 0); `make verify` exit 0;
  real check `<command>` exit 0 (<key line>). Mutations: <n> caught (<list>), 0 survived.
  Not verified: <what> because <why>.
```

`docs/TRACKER.md` row: `Status` = `doing`/`done`, `Who` = `DeepSeek`, and the same evidence in `Note`.
`done` requires the task's done-when, not just unit tests. A task whose done-when names a live system stays
`doing` until that live check runs or the owner accepts the blocker in writing.

### 1.5 Commit protocol

- One task, one commit, on `heimdall-build`, as `T##: what`. Docs and evidence for that task ride in it.
- Never `--no-verify`. Never push. Never rewrite history. Never commit a red tree.
- The pre-commit hook runs gitleaks on the staged diff and then `make fast`. Fake secrets in tests must be
  split or neutral (gitleaks scans history).
- A helper's red-stage work is never committed. Either finish it or park it in `PROGRESS.md`.
- The `heimdall` binary at the repo root is never committed.

### 1.6 Stop-the-line rules

Stop, write in `PROGRESS.md`, and ask — do not improvise:

1. Two failed attempts at the same error (R12).
2. A check must be weakened to proceed (R2).
3. The spec does not define the behaviour and a new event/state/field would be invented (T23 `on_fail` is
   the known case).
4. A live check needs credentials that are absent (write `not verified` + blocker; keep the task `doing`).
5. Disk below 2 GB, or a build needs data you would have to delete.
6. The task needs a file the owner guards (`.golangci.yml`, `Makefile`, `e2e/`, `testdata/real`, harness).

---

## 2. Ground truth (verify before believing)

Last commit: `092b72c T12: compose verification passed (all services healthy, healthz ok)` on
`heimdall-build`. Nothing pushed.

| Item | State |
|---|---|
| Committed and believed done | Stage 1 (`T100`–`T104`), `T00`–`T03`, `T07`, `T10`–`T14`, `T16`–`T18`, `T20`–`T22`, `T24`, `T25`, `T31`, `T32`, `T46`, `T98`, `T36` **not yet committed** |
| Real coverage | about **29/89 (~33%)**. `docs/TRACKER.md` says 5/89 — it is stale; do not trust its summary |
| Disk | 4.1 GB free, 97% used. Keep ≥5 GB. Prune only regenerable Docker/Go cache |
| Spend | OpenRouter $0.149 of the $3 testing cap. **No live LLM run in this stretch.** |
| Go | `$HOME/.local/go/bin`; the Makefile adds it too |

### 2.1 Half-done work in the tree (this is W0)

| Path | What it is | What is missing |
|---|---|---|
| `internal/adapters/tracker/jira/*` (`jira.go`, `adf.go`, `breaker.go`, tests) | **T36 complete in-tree.** 34 `Test*` functions, golden `testdata/kan/` | review, mutation check, evidence, commit, live T93 |
| `internal/adapters/switch/` | **T26 stubs** (`client.go`, `coordinator.go`, `messages.go`, `stream.go`) + fake + 17 red tests | all methods; tests for `Post`/`Read`/`Stream` |
| `internal/transport/mcp/` | **T28 stubs** (`api.go`, `rpc.go`) + `doc.go` + `fakes_test.go` (rig, **zero** tests) | all tests, then the handler/tools/bridge |
| `internal/app/engine/` | **T23 stubs** (`engine.go`, `step.go`) + `fakes_test.go` (14 steps, enforcing store, **zero** tests) | design note, all tests, engine |
| `docs/PROGRESS.md` | modified with T36/T04 notes | keep; fold into the T36 commit |
| `docs/HANDOFF.md` | untracked handoff | commit as docs in the first docs commit |
| `testdata/kan/` | synthetic ADF goldens for T36 | **stand-ins**, not real KAN tickets until T93 |
| `internal/adapters/github/live_test.go` | untracked live test from T04 | decide: commit with T04 evidence or park; it is test code |
| `heimdall` (repo root) | stray binary | **never commit**; add nothing to `.gitignore` without asking |

### 2.2 The red set at the start

1. `internal/app/engine/fakes_test.go` used `time.Date` in a `const`; already changed to `var` and gofmt'd
   by the previous session. Re-run `make fast` to confirm.
2. `golangci-lint` reports ~139 issues, almost all `unused`/`unused-parameter` in the T26/T28/T23 stubs.
   They clear when the stubs are real. **Never `//nolint`, never delete the tests.**
3. T26's 17 tests fail on `not implemented`. That is the intended red for W0.4.

### 2.3 Stale things you will meet

- `docs/TRACKER.md` summary (5/89) is wrong. The task rows are the useful part.
- `make prototype` prints `0/40 green, 38 blocked, 2 red`; the two reds are stale CLI expectations in
  P9.1/P9.4, not new product bugs. Do not edit scenarios to make them pass (R2).
- `make cost` and `make fixtures-push` exit 1 on purpose until T19 and T31 publish.
- `PROGRESS.md` still describes T17/T21/T20 as "in progress" in places; the commits (`b3d05be`, `928cd1a`,
  `41a0f4d`) are the truth.

---

## 3. Work breakdown

### 3.0 Rules for W-items

- **W0 finishes everything half-done before any new feature.** That is the owner's order and it also unbreaks
  `make fast`.
- Keep changes focused and reviewable. Work may span sessions; record partial progress in `PROGRESS.md`.
- **Write fence:** each item lists the directories it owns. Do not edit outside the fence. Anything else you
  notice goes to `PROGRESS.md` "Noticed" (R10).
- **Parallelism:** only after `make fast` is green and only on disjoint fences (section 6).
- **Dependencies are real:** do not claim dependent acceptance criteria are met until their prerequisites are met. Independent preparation or implementation may proceed; record what remains blocked.

| W | Task | Depends | Commit message |
|---|---|---|---|
| W0.1 | inventory + unbreak | — | (no commit; or `T36` includes the tree fix if it is the only red) |
| W0.2 | T36 Jira poll/ADF/breaker | T25; live proof T93 | `T36: Jira poll with overlap window, ADF walker and circuit breaker` |
| W0.3 | T93 live Jira spike | T07, T36 | `T93: live Jira poll, overlap window and dedupe spike` (or a written blocker, no commit) |
| W0.4 | T26 Switch client | T01, T11 | `T26: Switch client with rooms, references, messages and resumable stream` |
| W0.5 | T28 MCP server + bridge | T11 | `T28: MCP server and stdio bridge with auth, role checks and bounded reads` |
| W0.6 | T23 step engine | T21, T22 | `T23: step engine with default workflow, timers and replay` |
| W1 | T05 JUnit `<failure>` spike | — | `T05: missing-route seam tests fail as JUnit failure under gotestsum and pytest` |
| W2 | T29 parser half | T05, T12 | `T29: JUnit parser with red-first, AC coverage, flake and seam checks` |
| W3 | T39 fixture matrix | T05 | `T39: hermetic Go, Python, Node and JVM fixture matrix` |
| W4 | T34 detector half | T29 (parser), T15 not needed | `T34: repo profile schema, detectors and validator` |
| W5 | T40 reviewer skill | T11 | `T40: reviewer skill with evidence rules and schema-valid examples` |
| W6 | T41 resolver skill | T11 | `T41: resolver skill with citation rules and schema-valid verdicts` |
| W7 | T37 people mapping | T26, T32 | `T37: people mapping by Switch sender id and command decisions` |
| W8 | T19 budget/ledger | T18 | `T19: cost ledger, balance check and the $3 budget guard` |
| W9 | T42 context bundles | T24 | `T42: deterministic per-role context bundles with blob hashes` |
| W10 | T44 review validation | T28, T40 | `T44: review validation, citation checks, dedup and output filter` |

W2 and W3 can run in parallel (disjoint). W5 and W6 share `internal/testkit/skills`; run sequentially or
split by file. W9 and W8 are independent of everything else.

---

### W0.1 — Inventory and unbreak

**Goal:** the tree is understood and the only red is owned by W0.2–W0.6.

**Steps**

1. `git status --short`, `git log --oneline -3`, `df -h /`.
2. `export PATH=$HOME/.local/go/bin:$PATH; make fast` — save the full log, record the exact red set.
3. Confirm the untracked list matches section 2.1. If a file changed under you, read it before touching it.
4. Do not delete `heimdall`, `docs/HANDOFF.md`, `testdata/kan/` or `internal/adapters/github/live_test.go`.
5. Write the red set into `PROGRESS.md` under the current session.

**Acceptance:** every remaining red is named in section 2.2 or explained in `PROGRESS.md`. No unrelated
package edited.

---

### W0.2 — T36: Jira poll, ADF walker, circuit breaker

**Spec:** `TECH_SPEC.md` 13 (Jira), 2.5 (resilience), D97/V55; `T25` for the tracker port.
**Depends:** T25 (done). **Real proof:** T93 (W0.3).
**Read first:** `internal/adapters/tracker/jira/doc.go` (or the package comment), `jira.go`, `adf.go`,
`breaker.go`, every `_test.go`, `testdata/kan/README` if present, `TECH_SPEC.md` 13.
**Fence:** `internal/adapters/tracker/jira/`, `testdata/kan/`, the T36 bullet in `PROGRESS.md`.

The work is in the tree. **Do not rewrite it.** Review it against the spec, then verify:

| Claim | Check |
|---|---|
| `Changes` re-queries from `cursor − 10 min` | a test moves the clock and asserts the JQL/updated bound |
| dedupe by `(key, changelog id)` | the same changelog id twice emits one `change.triggered` |
| a failed poll does not advance the cursor | inject a 5xx, assert the next poll overlaps |
| `Ticket` parses ADF to text and the bullets under an "Acceptance criteria" heading | golden file asserts the AC text |
| each criterion hashed SHA-256 and numbered `AC1..n` in order | test asserts the hash and order |
| an assignee-less ticket is refused | test asserts the error |
| 429 honours `Retry-After` | fake clock/transport asserts the pause |
| 5xx retries 3 times with doubling | test counts the attempts |
| auth/404 are not retried | test counts one attempt |
| `Retry-After` > 2 min returns unavailable at once | test asserts no pause |
| cancelled ctx stops the pause | test cancels and asserts `context.Canceled` |
| breaker opens after 5 unavailable, sends nothing while open, one probe after 30 s, re-opens on failure, closes on success, client errors do not count | `resilience_test.go` |
| ADF walker refuses malformed/non-ADF and never panics | fuzz test present; if not, add one |

**Commands**

```bash
go test -count=1 ./internal/adapters/tracker/jira/...
go tool -modfile=tools.mod golangci-lint run ./internal/adapters/tracker/jira/...
```

**Mutation check (5, one at a time, each must go red):** overlap window; dedupe key; retry count;
breaker limit; ADF heading match.

**Real check:** deferred to W0.3. Until it runs, T36 stays `doing` in `TRACKER.md` with
`not verified: live KAN poll` and the synthetic goldens are named as stand-ins, never as real tickets.

**Acceptance:** package tests exit 0; lint `0 issues`; `make fast` exit 0; 5 mutations caught; evidence
written. **Commit** only after that.

**Forbidden shortcuts:** hardcoding KAN-1/KAN-2 fixtures; treating a changelog-less issue as new without the
spec's rule; a poll that advances the cursor on error; a breaker that counts 4xx; a sleep instead of the
injected clock/wait.

---

### W0.3 — T93: live Jira spike

**Spec:** S20, D97/V55; `TECH_SPEC.md` 13. **Depends:** T07, T36.
**Fence:** `internal/adapters/tracker/jira/live_test.go` (new, `//go:build live`), `docs/PROGRESS.md`.

**Work**

1. Follow `internal/adapters/github/live_test.go` for the pattern: read `.env` **inside the process only**
   (a local `readDotEnv`), skip if the Jira values are absent.
2. Create a test ticket in `KAN` with an assignee and an "Acceptance criteria" ADF description; move it to
   In Progress; poll `Changes`; assert exactly one `change.triggered` with the right AC.
3. Move a second ticket and poll immediately (index lag); poll again after the overlap window; assert the
   late move is seen once, not twice.
4. Clean up: transition to Done or delete if the token allows; otherwise label `heimdall-test` and record it.
5. Record the exact JQL, the poll timings and the two dedupe keys in `PROGRESS.md`. Never print the token or
   the email.

**If the token cannot create/move tickets:** do not fake it. Write
`blocked: Jira token cannot create/move tickets` in `PROGRESS.md`, keep T36/T93 `doing`, and ask the owner
for a test project/board. No commit for a blocked spike unless the live test file is useful and skipped
honestly.

**Acceptance:** `go test -tags live -count=1 -run TestTask_T93 ./internal/adapters/tracker/jira/` exit 0, or
the written blocker.

---

### W0.4 — T26: Switch client

**Spec:** `TECH_SPEC.md` 13 (Switch), 2.5 (deadline/breaker), 6.2/6.9 (room/stream); `MVP_SCENARIOS` P1.2,
P3.1. **Depends:** T01, T11. **Done when:** room opens from the engine; replies become events.
**Read first:** the whole of `internal/adapters/switch/fake_test.go` (the protocol contract), then the 17
tests in `client_test.go`/`coordinator_test.go`, then `doc.go` if present.
**Fence:** `internal/adapters/switch/` only.

**Existing red (do not rewrite):** 17 `Test*` functions covering `New`, bearer from the secret file,
401→`ports.ErrUnauthorized`, 404→`ports.ErrNotFound`, 5xx→`ErrUnavailable`, `StatusOf`, 10 s deadline and
cancellation, breaker open/probe, registration, room create/adopt/bridge, invite, references, key redaction.
They currently fail on `not implemented`; make them pass for the right reason.

**New red tests to write first** (R1), in `messages_test.go` and `stream_test.go`:

*`Post`:*
- sends `{room_id, body}` to `/message` with the key file's bearer;
- the same effect key is not posted twice in one process (in-memory `seen` map);
- 401 is `ErrUnauthorized`, 5xx is `ErrUnavailable`.

*`Read`:*
- decodes `read_context` threads into `[]ports.Message` in timestamp order;
- the cursor is the newest entry's RFC3339Nano timestamp and is passed back as `since`;
- a malformed sender or missing timestamp is refused, not dropped;
- `truncated: true` is `ErrUnavailable` (the caller must re-read, not lose messages).

*`Stream`:*
- sends `Accept: text/event-stream`, `Last-Event-ID` from the resume cursor, `X-Switch-Connection-Id` from
  the connection state;
- `message` frames become `Delivery{Message, Sequence, Replayed, Cursor}`;
- a `connection_state` frame sets the heartbeat interval and generation; beats are posted at that interval
  (drive it through the fake's `beatCh`; never `time.Sleep` in the test);
- reconnect after a drop resumes from the last sequence and marks replayed messages `Replayed: true`;
- a gap (cursor out of the window) is surfaced as an error the caller can act on;
- eviction (`ErrEvicted`) stops the stream and does not reconnect;
- ctx cancellation returns `context.Canceled` and stops the beats.

**Implementation constraints**

- Read the key file on every call (rotation); set `Authorization: Bearer`, `Content-Type: application/json`.
- 10 s `context.WithTimeout` unless the caller already has a shorter deadline.
- Decode `{"result": ...}`; map statuses; record the breaker.
- `StatusOf` exposes the HTTP status for non-2xx client errors so `errors.As` finds it.
- Breaker: 5 unavailable opens, 30 s cooldown, one probe, success closes, client errors do not count. Copy
  the Jira pattern or extract `internal/adapters/internal/breaker` **only if** it needs no app/transport
  import; do not copy-paste 50 lines twice and do not add a dependency.
- `OpenRoom`: `list_rooms` first, adopt an unarchived room with the same name; else `create_room` with
  `{name, agent_names:[agentName], channel_type:"channel_public", bridge_id}`. Return `change.RoomID`.
- `RegisterCoordinator`: registration token, `{agent_type, name, description}` → `{id, api_key}`; `Key`
  redacts in `String()`/`Format`.
- `Post`: dedupe by `change.EffectKey`; the server also dedupes, but the adapter must not double-post.
- `Read`: flatten `root` + `replies`; validate each with `ports.Message.Validate`; cursor = latest.
- `Stream`: long-lived GET **without** the 10 s deadline; SSE via `bufio.Scanner` (`id:`, `event:`,
  `data:`, blank line); skip control frames; beats in a goroutine that stops with ctx; reconnect with the
  last sequence; stop on `ErrEvicted`.

**Commands**

```bash
go test -count=1 ./internal/adapters/switch/...
go tool -modfile=tools.mod golangci-lint run ./internal/adapters/switch/...
make fast
```

**Mutation targets:** status mapping; breaker threshold/cooldown; room adoption (must not create a second);
cursor newest-vs-oldest; resume header; eviction handling.

**Real check:** register the coordinator against the real Switch bridge, open a throwaway room, archive it.
Use the port-forward in `docs/RUNBOOK.md`. Never print the key. If the bridge is unreachable, write
`not verified: live Switch room` and keep the task `doing`.

**Forbidden shortcuts:** returning a canned room id; skipping the adoption path; a stream that reconnects on
eviction; eating the gap; a `time.Sleep` heartbeat; posting without the key file; logging the key.

**Commit:** `T26: Switch client with rooms, references, messages and resumable stream`.

---

### W0.5 — T28: MCP server and stdio bridge

**Spec:** `TECH_SPEC.md` 14.1, 6.2, 6.5, 15.4, 15.2. `internal/transport/mcp/doc.go` is the design note and
states the invariants. **Depends:** T11. **Done when:** the test-author calls tools from OpenCode.
**Read first:** `doc.go`, `api.go`, `rpc.go`, all of `fakes_test.go` (rig: `newRig`, `rpc`, `call`, `post`,
`decodeReply`).
**Fence:** `internal/transport/mcp/` only.

**Red tests to write first** (the doc names the files; the rig exists):

*`server_test.go`:* `initialize`; `ping`; `tools/list` filtered by role; unknown method; notification gets
202; JSON-RPC envelope shape; batch refused.

*`http_test.go`:* no token → 401 + `WWW-Authenticate: Bearer`; unknown/revoked token → 401; `Tokens.Resolve`
error → 503; body > 8 MiB → 413; method other than POST → 405; the token never appears in the response body
or the captured log.

*`tools_test.go` (one subtest per tool):*
- `heimdall_get_task`: envelope returned; role mismatch refused; no task refused.
- `heimdall_ack`: inside 60 s accepted; after 60 s refused; repeat accepted and changes nothing.
- `heimdall_get_change`: change view returned, role-scoped.
- `heimdall_read_untrusted`: only a ref named by the agent's own envelope; other refs refused; wrapped;
  capped.
- `heimdall_read_code`: test-author base SHA only; reviewer/resolver base or head; missing file
  `ErrNotFound`; path traversal refused.
- `heimdall_submit_tests`: validates with `testset.Submission.Validate` against
  `tests/heimdall/<change>` and the envelope's AC ids; invalid returns structured errors as an `isError`
  result; a late submission is passed `late`.
- `heimdall_ask_humans`: at most 3 questions; each non-blank and bounded.
- `heimdall_report_progress`: 400 chars max; at most one per 2 min per task (fake clock).
- A tool the role does not have returns the same answer as an unknown tool.

*`untrusted_test.go`:* fixed preface; 128-bit random boundary; content between boundary lines; capped with
a clear truncation note; a canary/secret-looking input is **not** filtered (the output filter is not built;
record it).

*`bridge_test.go`:* against a real `httptest.Server`; relays lines; preserves ids; writes a JSON-RPC error
when the server is unreachable or 401; exits when input ends or ctx is cancelled; never writes logs to `Out`.

**Implementation constraints**

- `HashToken`: `sha256.Sum256`, lowercase hex.
- `NewServer`: refuse nil `Logger`, `Clock`, `Tokens`, `Tasks`, `Code`, `Blobs`.
- `Handler`: POST only; strip `Bearer `; hash; `Tokens.Resolve`; 401/503; `http.MaxBytesReader` 8 MiB;
  decode one `rpcMessage`; reject arrays; dispatch.
- `tools/list`: static table filtered by role.
- `tools/call`: decode `{name, arguments}`; role lookup; `Tasks.Current`; check
  `assignment.Task.Role == principal.Role`; dispatch; every tool error becomes
  `toolResult{IsError:true, Content:[{Text: msg}]}` except protocol errors; log the cause, never the token or
  the arguments.
- Untrusted wrapper: 16 random bytes from `crypto/rand`, boundary line, preface, cap at
  `ports.MaxUntrustedText`.
- `Bridge`: `bufio.Reader.ReadString('\n')`, one POST per line with `Authorization`, copy the response body
  line to `Out`; on transport/401 build a `rpcReply` with the request id and `codeUnreachable`/
  `codeUnauthorized`; never log the token.

**Commands**

```bash
go test -count=1 ./internal/transport/mcp/...
go tool -modfile=tools.mod golangci-lint run ./internal/transport/mcp/...
make fast
```

**Mutation targets:** role table; 60 s ack window; 8 MiB body limit; `read_code` SHA rule; untrusted cap;
bridge error id.

**Real check:** a real `curl` against the handler with a test token (no secrets). OpenCode client smoke is
**not verified** until T27/T105 exist.

**Forbidden shortcuts:** special-casing the rig's token; returning the whole blob uncapped; role checks only
in `tools/list`; logging arguments; a bridge that exits on the first error without replying.

**Commit:** `T28: MCP server and stdio bridge with auth, role checks and bounded reads`.

---

### W0.6 — T23: step engine and default workflow

**Spec:** `TECH_SPEC.md` 5.1/5.2, 3.2/3.3, 2.4; `internal/app/engine/step.go` is the step contract.
**Depends:** T21, T22 (both committed). **Done when:** the workflow runs on fakes end to end.
**Fence:** `internal/app/engine/` only.

This is the least mechanical item. **Write `internal/app/engine/doc.go` first** and pin the semantics there,
then the tests, then the code.

**Semantics to pin**

- `New` validates the workflow with `workflow.Validate` against `Steps` (unique `Type`), `Roles`, `Schemas`
  and `Conditions`. A missing condition/role/schema/step type, duplicate type, unknown `after`, cycle, or a
  retry edge that is not backwards is `workflow.ErrPolicyInvalid`.
- `Advance(ctx, changeID, incoming)`:
  1. `Store.Load`; append the incoming events as the first transaction (they are the trigger).
  2. Build `ChangeView{ID, Events}` with versions as they will be stored.
  3. For steps in declaration order: skip a step whose `when` is false (counts as done); skip a step whose
     `after` is not all done/skipped; skip a step whose `On` trigger does not match.
  4. For a ready, not-done step: call `Start`; collect `Events`, `Effects`, `Timers`; assign versions;
     append **once** with `expected = len(stored)`; update the view. **One transaction per `Advance`.**
  5. A done step is skipped and its timers are not re-emitted.
  6. Return `Outcome{Version, Events, Effects, Timers, Complete}`; `Complete` means every step is done/skipped.
- `OnEvent`: offer each incoming event to every ready, not-done step; append in one transaction.
- `Timeout`: call `OnTimeout` only if the step is ready and not done; append; continue the normal run.
- **Failure/retry (the hard part).** `on_fail` is data. The simplest defensible rule: a step's failure is an
  event it emitted carrying a failure fact (`slop.evaluated{pass:false}`, `tests.verified` with a failed
  run). Then follow `OnFail`: `retry` re-runs the retried step's `Start` with the failure event in the view
  and lets the step decide; `then: escalate` emits the escalation event; `then: reject` emits
  `change.rejected`. **If the domain catalog has no event for a retry marker, do not invent one silently.**
  Stop, write the choice in `doc.go` + `PROGRESS.md`, and either park the item or amend `TECH_SPEC` in the
  same commit with the owner's yes (D134).
- Replay: `Advance` with no incoming on a stored change makes the same decisions and appends nothing. Test it
  twice and compare the store.

**Tests to write (`engine_test.go`, table-driven)**

- `New` refuses: empty workflow, duplicate step type, missing implementation, missing role, missing schema,
  missing condition, unknown `after`, cycle, retry that points forward.
- Happy path: `triggered → ... → gate.evaluated` on the 14 fakes; assert the exact event-type sequence, one
  append per `Advance`, and `Complete`.
- `when: sensitive_domain` false skips `signoff` and later steps still run; true runs it.
- `On: jira.transitioned` starts only on that event; `On: any` starts on any.
- `Start` idempotent: a second `Advance` does not call `Start` again for a done step.
- Timers: `write_tests` 2 h; `pick` 15 m; `review` 30 m; `Timeout` calls `OnTimeout` and appends.
- `Store.Append` conflict is returned wrapped, not swallowed; no partial effect recorded.
- Effects ride with the events that produced them, in the same append.
- A failing step follows `on_fail` without losing the failure event.
- Replay: second `Advance` appends nothing.

**Commands**

```bash
go test -count=1 ./internal/app/engine/...
go tool -modfile=tools.mod golangci-lint run ./internal/app/engine/...
make fast
```

**Mutation targets:** dependency check; trigger match; `when` skip; version assignment; append count; timer
emission; retry edge; replay no-op.

**Real check:** none — fakes end to end is the done-when. Write `not verified: real adapters` with the
reason.

**Forbidden shortcuts:** hardcoding the 14-step sequence; special-casing the fake store; emitting timers on
every run; two appends per `Advance`; swallowing the version conflict; inventing a retry event without the
spec amendment.

**Commit:** `T23: step engine with default workflow, timers and replay`.

---

### W0 gate (before W1)

- T36, T26, T28, T23 committed (or parked with a written reason and the owner told).
- `make fast` exit 0.
- `make verify` exit 0, or a named pre-existing exception with the reason.
- No stub left in `internal/adapters/switch`, `internal/transport/mcp`, `internal/app/engine`.
- Lint 0 issues; the ~139 `unused` findings are gone because the code is real, not because of a suppression.

---

### W1. T05 — missing route is a JUnit `<failure>`, not `<error>`

**Spec:** `TEST_STRATEGY.md` G0/T05, `TECH_SPEC.md` 12.1/D39. **Why:** the red-first check must tell an
assertion (red) from a compile/import error (rejected).
**Fence:** `testdata/spikes/t05/` only. Throwaway code; no product code.

**Work**

1. `testdata/spikes/t05/go/`: a tiny module with (a) a test that calls a missing route on an
   `httptest.Server` and asserts 200 → `<failure>`, and (b) a test with an undefined identifier → `<error>`.
2. `testdata/spikes/t05/python/`: a pytest test asserting `status_code == 200` against a missing route.
3. Run and inspect:
   ```bash
   cd testdata/spikes/t05/go && go tool -modfile=../../../../tools.mod gotestsum --junitfile out.xml -- ./...
   cd ../python && pytest --junitxml=out.xml
   ```
4. Record the exact XML element lines in `PROGRESS.md`.
5. Do not commit `out.xml` (`.gitignore` under the spike dir or delete it).

**Acceptance:** both XMLs show the expected element; commands and output lines in `PROGRESS.md`.
**Forbidden:** making the test pass; asserting on pytest's exit code instead of the XML.
**Commit:** `T05: missing-route seam tests fail as JUnit failure under gotestsum and pytest`.

---

### W2. T29 (parser half) — JUnit, red-first, AC coverage, flake, seam imports

**Spec:** `TECH_SPEC.md` 12.1/12.2, D39, D98; `TEST_STRATEGY.md` G2/T29. **Depends:** T05, T12.
**Parent stays `doing`** until the container runner lands. Do not mark T29 done.
**Fence:** `internal/domain/junit/`, `internal/app/testquality/`, golden files under
`internal/domain/junit/testdata/`.

**Work**

1. `internal/domain/junit/` (stdlib only):
   - `Result{Tests []Test, Suites, Failures, Errors, Skipped, Time}`,
     `Test{Name, Class, Outcome, Message}`.
   - `Parse([]byte) (Result, error)`: `<testsuites>`, `<testsuite>`, `<testcase>`, `<failure>`, `<error>`,
     `<skipped>`; no child = passed; skipped = skipped; failure **and** error = refuse (do not guess);
     unknown elements ignored; malformed XML names the offset.
   - `Outcome` constants `passed/failure/error/skipped`; the app maps them to `ports.Outcome`. Domain cannot
     import ports.
   - Fuzz `Parse` for no panics; a valid parse re-encodes to the same counts.
2. `internal/app/testquality/` (app: domain + ports + stdlib):
   - `RedFirst(run junit.Result, expected []string) error`: every expected test present and `failure`; zero
     `error`; missing/passed/skipped/error refused with the test name.
   - `ACCoverage(sub testset.Submission, acIDs []string) error`: every AC has a test name; every name exists
     in the submission's `AcceptanceCriteria`; tag syntax from the profile's `ac_tag` (default
     `// heimdall:{ac}`), checked in the file content.
   - `Flake(runs []junit.Result) []string`: names whose outcome differs across runs; 3 runs is the policy;
     fewer than 3 refused.
   - `SeamImports(files []testset.File, allowed []string) error`: `go/parser` imports compared by prefix; a
     test importing the implementation package directly is refused. Other languages out of scope; the
     profile declares its own rule.
3. Tests: table tests per function, including "error is never red-first", "one flaky test is quarantined",
   "an AC with no test fails", "an import outside the seam fails", "an allowed import passes", and a golden
   JUnit from gotestsum.

**Commands**

```bash
go test -count=1 ./internal/domain/junit/... ./internal/app/testquality/...
make fast
```

**Mutation targets:** outcome precedence (failure vs error), missing-test branch, 3-run minimum, tag parse,
import prefix.
**Real check:** the T05 XML is the real parser input. `not verified: runner integration`.
**Commit:** `T29: JUnit parser with red-first, AC coverage, flake and seam checks`.

---

### W3. T39 — hermetic fixture matrix

**Spec:** `TECH_SPEC.md` 20, D116/D149; `TEST_STRATEGY.md` G2/T39; `testdata/repos/README.md`.
**Depends:** T05. **Done when:** detectors/profiles are right on every fixture (T34 uses them).
**Fence:** `testdata/repos/{python,node,jvm}-demo/`, `internal/testkit/fixtures/`. Do **not** touch
`testdata/repos/pay-demo/` or `e2e/`.

**Work**

1. Create the three repos in the existing layout: `base/`, `branches/<name>/` with `branch.yaml`
   (parent, commit_message, delete). Each has one known red and one known green test on `main`, and a branch
   that fixes the red one.
   - Python: one HTTP service with a seam; a Postgres service is optional but the profile must declare it.
   - Node: standard library only, no `npm install`, so it runs offline.
   - JVM: one tiny Java file with `javac`/`java`, no Maven/Gradle download.
   - `profile.yaml` per repo matching the T34 profile (language, image, commands, seam, `ac_tag`).
2. Extend `Materialize` to any repo in the matrix; keep the deterministic author/date; add `Repos()` and
   `Profile(repo)`.
3. Tests:
   - untagged `matrix_test.go`: every repo has `base/`, a valid `branch.yaml` graph, a decodable profile, the
     expected red/green tests; materialising twice gives identical tree hashes.
   - `//go:build component` `matrix_component_test.go`: run each repo's own test command in its pinned image
     (`python:3.12-slim`, `node:22-slim`, `eclipse-temurin:21-jdk`, `golang:1.27`); red fails, fixed branch
     passes. These run in `make verify`.
   - `make fast`-safe structure tests for `ac_tag`/`seam`.

**Commands**

```bash
go test -count=1 ./internal/testkit/fixtures/
go test -tags component -count=1 ./internal/testkit/fixtures/
make verify
```

**Mutation targets:** repo detection, branch parent order, profile image, test command, tree-hash stability.
**Forbidden:** a fixture whose "red" test is red because it does not compile; a profile that downloads at
test time; editing the guarded story.
**Commit:** `T39: hermetic Go, Python, Node and JVM fixture matrix`.

---

### W4. T34 (detector half) — profile schema, detectors, validator

**Spec:** `TECH_SPEC.md` 12.2b, D98/D99/D153, 12.2; `TEST_STRATEGY.md` G2/T34. **Depends:** T29 (parser),
T15 not needed for this half. **Parent stays `doing`.**
**Fence:** `internal/domain/profile/`, `internal/app/profile/`, `internal/adapters/profileyaml/`.

**Work**

1. `internal/domain/profile/` (stdlib only): `Profile` with `Languages`, `Image`, `Setup`, `Test`,
   `Acceptance`, `Seam`, `ACTag`, `FetchHosts`, `Services`, `Evidence`, `Paths`; `Service{Name, Image, Env}`;
   `Evidence{ID, Run, Tier}`; `Paths{Tests}`.
   - `Validate(allowlist)`: required fields; image pinned by digest and allowlisted; `acceptance` contains
     `{dir}`; `ac_tag` contains `{ac}`; service images allowlisted; `fetch_hosts` are host names (no
     scheme/path); `evidence.run` is a bounded command; no `network` field.
   - `Allowlist`: default Go/Python/Node/JVM images by digest + `Contains`.
2. `internal/adapters/profileyaml/`: strict `KnownFields` decode into `profile.Profile`, then `Validate`.
3. `internal/app/profile/` detectors: `Detect(tree) Draft` for `go.mod`, `pyproject.toml`/`requirements.txt`,
   `package.json`, `pom.xml`/`build.gradle`, `Cargo.toml`, `Dockerfile`, `docker-compose.yml`; sorted,
   deterministic. `SuggestSeam`: `app.New` (Go), `backend.main:app` (FastAPI), `src/index.js` (Node), `Main`
   (JVM); no seam = a validation error, not a guess.
4. Tests: golden tree from `testdata/repos/*` → expected profile; every marker; a two-language monorepo gets
   both; bad image/network/fetch host refused; no seam refused; YAML round-trip.

**Commands**

```bash
go test -count=1 ./internal/domain/profile/... ./internal/app/profile/... ./internal/adapters/profileyaml/...
make fast
```

**Mutation targets:** marker detection, seam choice, digest pin, `{ac}` check, sorted output.
**Real check:** run the detector on `testdata/repos/pay-demo` and print the draft; compare with the golden.
**Commit:** `T34: repo profile schema, detectors and validator`.

---

### W5. T40 — reviewer skill

**Spec:** `TECH_SPEC.md` 6.4, 6.5, 8, 11.1, 14.1; `TEST_STRATEGY.md` G3/T40. **Depends:** T11.
**Fence:** `skills/reviewer/`, `internal/testkit/skills/`.

**Work**

1. `skills/reviewer/SKILL.md` (markdown): role/goal; read the task bundle first; never restate the diff; no
   style nits unless policy enables them; evidence tiers E1 repro test, E2 tool result, E3 confirmed memory,
   E4 reasoning (never blocks); every claim cites `file:line`, run id, memory ref or pack ref; untrusted
   content only through `heimdall_read_untrusted`; anti-sycophancy (never withdraw without changed evidence);
   output one `Review@1` JSON object; `clean` only when `findings` is empty; the `FindingIn` fields/limits;
   progress at most once per 2 min, 400 chars, no early findings; a worked example with three findings (one
   per tier) and a clean example.
2. Fenced ```json examples so a test can extract them.
3. `internal/testkit/skills/skills_test.go`: read the skill; extract JSON blocks claiming `Review@1`;
   `json.Unmarshal` into `finding.Review`; `Validate()`; bad example fails; the skill names the five tools it
   may call and forbids the rest; the evidence tier names and the `clean` rule are present.

**Commands**

```bash
go test -count=1 ./internal/testkit/skills/
make fast
```

**Real check:** live agent smoke deferred (needs T27). `not verified: live agent smoke`.
**Commit:** `T40: reviewer skill with evidence rules and schema-valid examples`.

---

### W6. T41 — resolver skill

**Spec:** `TECH_SPEC.md` 11.3. Same shape as W5. **Fence:** `skills/resolver/`, `internal/testkit/skills/`
(the resolver test file; coordinate with W5, they share the directory).

**Content**

- Input: the case file (disputed finding, both sides, cited evidence), memory with `as_of`, the AC, code
  around the disputed lines.
- Output: one `Verdict@1`: `decision` uphold/dismiss/amend/spec_change/needs_human; rationale 10–1000 chars;
  1–16 citations; confidence; `needs_human` when evidence is insufficient or the domain is human-only.
- Rules: a verdict is a recommendation, never a state change; cite every ref; never invent a run/memory ref;
  no raw chat; state what evidence would change the verdict.
- Examples: uphold, dismiss, needs_human. Test validates with `deliberation.Verdict.Validate`.

**Acceptance:** package test + `make fast`; live smoke `not verified`.
**Commit:** `T41: resolver skill with citation rules and schema-valid verdicts`.

---

### W7. T37 — people mapping and command decisions

**Spec:** `TECH_SPEC.md` 13 (People), D95/D140/D158; `MVP_SCENARIOS` P3.1/P4; `T32` (parser, done).
**Depends:** T26 (W0.4), T32. **Done when:** wrong-person, agent-sender and stale-SHA attempts are refused
in tests.
**Fence:** `internal/domain/policy/`, `internal/app/commands/`, `cmd/heimdall/connect.go`.

**Work**

1. `policy`: `LookupChat(sender) (name, Person, ok)` exact match on `Person.Chat`; `LookupGitHub(login)`;
   validation refuses two people sharing `Chat` or `GitHub`.
2. `internal/app/commands/decide.go`: `Decide(policy, cmd, sender, now) (Decision, error)`. Refuse an
   unmapped sender, an agent sender (no chat match for agents; `@switch-agent-` prefix), a verb the person's
   role does not allow, a stale-SHA approval (SHA must equal the head passed in). Return
   `Decision{Kind, Person, Reason}`.
3. `connect people`: interactive list → name → one recent room post through an injected `PeopleSource`;
   store `people.<name>.chat`; atomic TOML write via the existing `writeSelection` (extend to nested keys);
   `--json`, `--name`, `--sender`; never a secret, never print a token.
4. Tests: duplicate chat/github refused; exact match only; a name without `chat` not found; mapped human
   allowed; unmapped refused with "link your account"; agent sender refused; wrong role refused; stale SHA
   refused; valid approval allowed; golden TOML; atomic write leaves the file unchanged on error; no secret
   in output.
5. Real check: post as `dev-author` in a test room and run `connect people`. If the port-forward is down,
   `not verified: live Switch sender id`.

**Commands**

```bash
go test -count=1 ./internal/domain/policy/... ./internal/app/commands/... ./cmd/heimdall/...
make fast
```

**Mutation targets:** exact match vs prefix; duplicate detection; agent prefix; stale SHA; role verb table.
**Commit:** `T37: people mapping by Switch sender id and command decisions`.

---

### W8. T19 — LLM budget guard and cost ledger

**Spec:** `TECH_SPEC.md` 6.6, D113/D128/D161; `TEST_STRATEGY.md` 4. **Depends:** T18.
**Done when:** `make e2e` refuses to start with too little balance; every live run adds a ledger line.
**Fence:** `internal/app/budget/`, `internal/adapters/ledger/`, `internal/adapters/openrouter/`,
`cmd/heimdall/cost.go`. **`Makefile` is guarded — ask the owner.**

**Work**

1. `internal/app/budget/`: `LedgerEntry{RunID, Purpose, Model, PromptTokens, CompletionTokens, CostUSD, At,
   Note}`; `Ledger` interface; `Guard{CapUSD, PerPurpose, Ledger, Balance}` with
   `Check(purpose, estimate)` refusing at the $3 cap, over the purpose budget, or below the live balance.
   The error names the numbers and the cap. Stop rule: 2× estimate (D161).
2. `internal/adapters/ledger/`: JSONL, one entry per line, `O_APPEND`, no package state; `Total` scans; a
   corrupt line is an error naming the line, not skipped.
3. `internal/adapters/openrouter/`: read-only `Balance` client (`GET /api/v1/credits` or the key endpoint;
   use the one that works with the production key); bearer from a `Secrets` interface; 10 s deadline; no
   retry storm; 401/5xx → `ports.ErrUnauthorized`/`ErrUnavailable`.
4. `cmd/heimdall/cost.go`: `heimdall cost [--json]` prints total spent, remaining to $3, per-purpose totals,
   live balance. A missing key or unreachable API is an error, not a zero.
5. `Makefile`: add `cost: ## remaining LLM budget per purpose (T19)`; wire the guard into `e2e` **only where
   the TODO says**. If the owner declines, leave it and record it.
6. Tests: ledger append/read/corrupt line; guard at cap/over purpose/below balance; fake OpenRouter returns
   a balance and 401; `cost --json` shape; no key in output or errors.
7. Real check: `heimdall cost` against the real key inside the Go process; spend is $0.149; record the real
   number. **Do not run an LLM.**

**Commands**

```bash
go test -count=1 ./internal/app/budget/... ./internal/adapters/ledger/... ./internal/adapters/openrouter/... ./cmd/heimdall/...
make fast
make cost
```

**Mutation targets:** cap comparison, purpose budget, balance check, ledger append mode, 401 mapping.
**Commit:** `T19: cost ledger, balance check and the $3 budget guard`.

---

### W9. T42 — context bundles

**Spec:** `TECH_SPEC.md` 6.4, 6.3, D45; `ports.BlobStore`. **Depends:** T24 (done).
**Done when:** bundle hash recorded; replay gives an identical bundle.
**Fence:** `internal/app/bundle/`.

**Work**

1. `Request{Role, Change, BaseSHA, HeadSHA, Diff, FilesChanged, Criteria, CI, Untrusted, Pack, Memory,
   BudgetTokens}`.
2. `Build(req) (Bundle, error)`: stable parts first (skill, role rules, change), then diff/files/AC/CI/
   untrusted; canonical JSON (sorted keys, no HTML escaping, fixed separators); cap by role budget
   (test-author 40k, reviewer 60k, resolver 30k tokens; documented chars/4 estimate; **refuse over the cap,
   do not truncate silently**); each untrusted part is a reference, never inline text.
3. `Hash(bundle) workflow.BlobRef` = `blob:sha256:<hex>`; `Store(ctx, blobs, class, bundle)` (Put then hash,
   wrap errors).
4. Replay = `Build` with the same request; the test compares hashes.
5. Tests: same request → same hash; changed criteria/diff/pack → different; over-cap refused with role and
   numbers; untrusted refs are refs; two reviewers with different packs get different bundles; blob
   round-trip; missing blob `ErrNotFound`.

**Commands**

```bash
go test -count=1 ./internal/app/bundle/...
make fast
```

**Mutation targets:** canonical ordering, cap, untrusted inline, hash input, role budget.
**Real check:** none; `not verified: live reviewer bundle` (needs T27/T43).
**Commit:** `T42: deterministic per-role context bundles with blob hashes`.

---

### W10. T44 — `submit_review` validation, dedup, output filter

**Spec:** `TECH_SPEC.md` 6.5, 14.1, 15.2, 11.1; `finding.Review`. **Depends:** T28 (W0.5), T40 (W5).
**Done when:** an invalid finding is returned with errors; rejected only when the agent runs out of budget.
**Fence:** `internal/app/review/`; MCP wiring only if W0.5 is done.

**Work**

1. `internal/app/review/`: `Refs` interfaces (`FileExists`, `LineCount`, `RunExists`, `MemoryExists`,
   `PackExists`); `Submit` validates `finding.Review`, then every location exists at head and the range is
   inside the file; every evidence ref exists and belongs to the change/caller; a finding outside the diff
   needs a compat/ops/security category and a reason; dedup by overlapping file+lines and normalized-token
   Jaccard ≥ 0.6 (merge reporters, keep the strongest evidence); return **all** problems joined with field
   paths as an `isError` the agent can act on.
2. `Filter` for progress text: secret patterns, canary list, 400 chars; a hit is refused with a generic
   message and the cause logged, never returned.
3. `Calibrate` out of scope; consensus is T60.
4. Tests: valid accepted; unknown file/line/ref refused with the field path; outside-diff without a
   compat/ops/security category refused; overlapping findings merged with both reporters; 0.6 boundary;
   clean review with findings refused; huge review refused by `Validate`; the filter catches a fake token and
   a canary (split literals so gitleaks does not flag them).
5. Wire into MCP `heimdall_submit_review` **only** if W0.5 is done; otherwise record
   `not verified: MCP wiring`.
6. Real check: none (fake tree/refs); `not verified: live reviewer submission`.

**Commands**

```bash
go test -count=1 ./internal/app/review/...
make fast
```

**Mutation targets:** path check, line bound, Jaccard threshold, merge reporters, filter leak.
**Commit:** `T44: review validation, citation checks, dedup and output filter`.

---

## 4. Phase C — deferred (do not start in this stretch)

These need the server process, the VM, the runner or an agent runtime:

`T15` runner, `T27` agent runtime, `T29` container/runner half, `T30` test-author flow, `T34` profiler
skill/PR flow, `T35` narrowed tokens/doctor permissions, `T38` scripted agent, `T43` reviewer pick/fan-out,
`T45` evidence verification, `T47` merge-rule wiring/check publishing, `T60` challenge/resolver, `T61` test
sign-off, `T62` security eval, `T63` dashboard, `T64` boot reconciliation, `T65` chat memory, `T66`
baselines, `T67` Teams, `T68` local mode, `T69` planted-issue check, `T71` reliability, `T72`/`T99` setup
wizard, `T74` chaos, `T75` Jurist AI, `T80`–`T84` ship, `T105` deploy, `T16b` sandbox, `T91`/`T94` VM
spikes, `T09` Teams.

If the owner asks for one, it becomes its own W-item with its own spec read. Do not bolt it onto another
task.

---

## 5. Coverage accounting (be honest)

Count only tasks with their own tests, lint, evidence and commit.

| Bucket | Counted tasks | Running total |
|---|---|---|
| Done at `092b72c` | ~29 | 29/89 (~33%) |
| W0 | T36, T26, T28, T23 (+T93 if it runs) = 4–5 | 33–34/89 (~37–38%) |
| W1–W10 | T05, T39, T40, T41, T37, T19, T42, T44 = 8 | 41–42/89 (~46–47%) |
| Halves not counted | T29 parser, T34 detector stay `doing` | — |

So this plan honestly lands around **46–47%**, not 60. Reaching 60 needs about twelve more tasks (for example
T35, T47, T63, T105, T43, T45, T64, T38, T30, T61, T27, T15). Never count a half-task, a stub, or an
untested package. Report the number with its definition.

---

## 6. Subagent protocol (strict)

Use subagents only for W1–W10, only on disjoint fences, and only after `make fast` is green. W0 is done by
the lead (the half-done work needs judgment and the red is already there).

### 6.1 Prompt preamble (paste into every subagent prompt)

```
You are a helper on the Heimdall repo at /home/aarush_linux/projects/switch_shi.
Read, in this order: AGENTS.md, docs/CODING_STANDARDS.md, docs/TEST_STRATEGY.md sections 2, 3 and 5,
docs/DEEPSEEK_PLAN.md section 1 and the W-item below, and the doc.go of every package you touch.

Rules:
- Write the failing test FIRST and run it. Paste the assertion failure in your report. A compile error is
  not a red. If you wrote code before the test, say so; the lead will reject the batch.
- Then the smallest code that passes. No stubs, no canned fixture answers, no fallbacks, no test-mode
  branches, no time.Sleep, no fmt.Print*, no os.Getenv outside internal/platform/config, no //nolint, no
  skipped tests, no TODO for your own scope.
- Do not commit. Do not run git commands other than `git status` and `git diff`.
- Do not touch go.mod, go.sum, Makefile, .golangci.yml, docs/harness, .claude, e2e/, testdata/real, or any
  file outside your write fence.
- Do not read .env, secrets/, tokens or keys.
- Run: export PATH=$HOME/.local/go/bin:$PATH; go test -count=1 ./<your packages>/...;
  go tool -modfile=tools.mod golangci-lint run ./<your packages>/...
- Report exactly: files added/changed, test names, red output, commands + exit codes, lint result, and what
  is not verified.
A plausible report is not proof; the lead re-runs everything.
```

### 6.2 Batches and fences

| Batch | W | Owns | Must not touch |
|---|---|---|---|
| A | W1 | `testdata/spikes/t05/` | everything else |
| B | W2 | `internal/domain/junit/`, `internal/app/testquality/` | `internal/ports` |
| C | W3 | `testdata/repos/{python,node,jvm}-demo/`, `internal/testkit/fixtures/` | `e2e/`, `testdata/repos/pay-demo` |
| D | W4 | `internal/domain/profile/`, `internal/app/profile/`, `internal/adapters/profileyaml/` | `internal/domain/policy` |
| E | W5 | `skills/reviewer/`, `internal/testkit/skills/reviewer_test.go` | `internal/domain/finding` |
| F | W6 | `skills/resolver/`, `internal/testkit/skills/resolver_test.go` | `internal/domain/deliberation` |
| G | W7 | `internal/domain/policy/`, `internal/app/commands/`, `cmd/heimdall/connect.go` | `cmd/heimdall/main.go` |
| H | W8 | `internal/app/budget/`, `internal/adapters/{ledger,openrouter}/`, `cmd/heimdall/cost.go` | `Makefile` (ask the lead) |
| I | W9 | `internal/app/bundle/` | `internal/ports` |
| J | W10 | `internal/app/review/` | `internal/transport/mcp` |

E and F share `internal/testkit/skills`; run sequentially or split by file. B and C are disjoint and can run
together after W1. H, I, J are independent; J needs W0.5 and W5.

### 6.3 Review protocol (the lead, after every subagent)

1. `git diff -- <fence>` — read every line. Look for test-only fallbacks, swallowed errors, `_ =` on errors,
   `time.Sleep`, `fmt.Print`, package globals, canned answers, a test written after the code.
2. Run the package tests `-count=1`, then `make fast`, then lint.
3. Mutation check: 5 one-line changes, one at a time; each must go red. A survivor is a missing test.
4. If green: `PROGRESS.md` + `TRACKER.md`, commit. If red: send the exact failure to the same subagent
   session; do not start a new one.
5. Never commit a helper's red-stage test. Partial work stays in the tree and is parked explicitly.

---

## 7. Evidence and tracker format

### 7.1 `PROGRESS.md` bullet

Use the template in section 1.4. Put it under the current session heading. Include the red assertion line,
the commands, the test count, the lint result, the mutation list, and `not verified`.

### 7.2 `TRACKER.md` row

`Status`: `doing` or `done`. `Who`: `DeepSeek`. `Note`: the same evidence. `done` only when the task's
done-when is met; a live done-when that did not run keeps it `doing` and says so.

### 7.3 Commit messages

```
T36: Jira poll with overlap window, ADF walker and circuit breaker
T26: Switch client with rooms, references, messages and resumable stream
T28: MCP server and stdio bridge with auth, role checks and bounded reads
T23: step engine with default workflow, timers and replay
T05: missing-route seam tests fail as JUnit failure under gotestsum and pytest
T29: JUnit parser with red-first, AC coverage, flake and seam checks
T39: hermetic Go, Python, Node and JVM fixture matrix
T34: repo profile schema, detectors and validator
T40: reviewer skill with evidence rules and schema-valid examples
T41: resolver skill with citation rules and schema-valid verdicts
T37: people mapping by Switch sender id and command decisions
T19: cost ledger, balance check and the $3 budget guard
T42: deterministic per-role context bundles with blob hashes
T44: review validation, citation checks, dedup and output filter
T93: live Jira poll, overlap window and dedupe spike
```

One commit per task. Docs/evidence ride with their task. `docs/HANDOFF.md` goes in the first docs commit.

---

## 8. Commands, traps and the local map

### 8.1 Commands (never invent others)

```bash
export PATH=$HOME/.local/go/bin:$PATH
make fast                 # fmt-check, vet, arch, lint, unit. Under 60 s
make verify               # + race, lint-selftest, harness, scripts, vuln, secrets
make lint                 # golangci-lint only
go test -count=1 ./internal/<pkg>/...
go tool -modfile=tools.mod golangci-lint run ./internal/<pkg>/...
make prototype ONLY=P2    # the board for one scenario
make e2e T=T24            # the real check for a task, if TestTask_T24 exists
make cost                 # exits 1 until T19
```

`make fast | tail` hides the exit status. Run it without a pipe or read the full log.

### 8.2 Traps (learned the hard way)

- `go list` exits 0 for a package that did not load; check its `Error` fields (archcheck does).
- gitleaks scans **history**: fake secrets in tests must not look like secrets (split or neutralize).
- The bash guard refuses redirects plus guarded names on one line, `make secrets`, and some regex shapes.
  Run one simple command per call; use `write`/`edit` for file changes.
- Guarded files: `.golangci.yml`, `AGENTS.md`, `CLAUDE.md`, `e2e/scenarios/*`, `e2e/stories/*`,
  `testdata/real/*`, `.github/workflows/*`, `Makefile`, `tools.mod`, `tools.sum`, `.gitleaks.toml`,
  `.gitleaksignore`, `scripts/*` (test scripts), `internal/archcheck/*`, `internal/lintselftest/*`,
  `testdata/lint/*`.
- A pending `question` tool call can hang an unattended session; do not leave one open.
- Disk: 4.1 GB free. Prune only Docker images/build cache and old Go cache. Ask before deleting repo data.
- `make verify` needs Docker. If Docker is down, say so; do not mark it green.

### 8.3 Existing file map (what is real, what is a stub)

| Area | Files | State |
|---|---|---|
| domain | `internal/domain/{change,finding,deliberation,testset,workflow,policy,slop,gate,id,...}` | real, committed |
| ports | `internal/ports/*.go` | real, committed |
| app | `internal/app/{intake,commands,outbox}` | real, committed |
| app | `internal/app/engine` | **stub** (W0.6) |
| adapters | `internal/adapters/{github,postgres,tracker/file,tracker/jira,policyyaml,registry}` | real, committed (jira uncommitted) |
| adapters | `internal/adapters/switch` | **stub + 17 red tests** (W0.4) |
| adapters | `internal/adapters/blobfs` | does not exist yet; `ports.BlobStore` is declared, T42 is the first consumer, not the adapter |
| transport | `internal/transport` (`health.go`, `doc.go`) | real, committed |
| transport | `internal/transport/mcp` | **stub + rig, 0 tests** (W0.5) |
| testkit | `internal/testkit/{fixtures,record,postgres}` | real; `skills` to add in W5/W6 |
| cmd | `cmd/heimdall`, `cmd/heimdall-server` | real (connect, doctor stub); `cost` to add |
| e2e | `e2e/`, `e2e/stories/` | guarded; do not edit |
| fixtures | `testdata/repos/{pay-demo,ledger-demo}` | real; add python/node/jvm |
| goldens | `testdata/kan/` | synthetic; T93 makes them real |

If a path above is wrong, fix the table in the same commit that finds it. The code is the truth.

---

## 9. Risk register

| Risk | Mitigation |
|---|---|
| Disk fills (4.1 GB free) | `df -h /` before big builds; prune only regenerable caches; ask before deleting anything else |
| Lint `unused` in the stubs | clears when the stubs are real; never suppress, never delete tests |
| T23 `on_fail` has no catalog event for retry | write the design in `doc.go`; if a new event is needed, amend `TECH_SPEC` in the same commit with the owner's yes, or park |
| T26's real Switch API differs from the fake | the fake is the contract; a live difference fixes adapter **and** fake together, recorded |
| T36 goldens are synthetic | never call them real KAN tickets; T93 is the proof; T36 stays `doing` until then |
| A subagent adds a dependency | refuse any `go.mod` change without the lead; brief says stdlib only |
| A subagent writes tests after code | reject the batch; rewrite the test first |
| `make fast` green but `make verify` red | `make verify` is the commit gate |
| Pre-commit rejects a red tree | fix the tree; never `--no-verify` |
| Gitleaks flags a fake secret | split/neutralize; no ignore without the owner |
| A guarded file must change | stop and ask; do not route around the hook |
| Spend | no live LLM in this stretch; T19 only reads the balance; cap $3, spent $0.149 |
| Helper session dies mid-task | same session id resumes; otherwise finish it or park it explicitly |

---

## 10. First 30 minutes of the next session

1. `git status --short && git log --oneline -3 && df -h /`.
2. Read section 0.1 in order.
3. `export PATH=$HOME/.local/go/bin:$PATH; make fast` — write the red set down.
4. Confirm the T36 tree still passes:
   `go test -count=1 ./internal/adapters/tracker/jira/...` and its lint.
5. Do **W0.2**: review T36, 5 mutations, evidence, commit `T36: ...`.
6. **W0.3** T93 live if credentials exist; otherwise write the blocker and move on.
7. **W0.4** T26. Do not start a low-brain batch until `make fast` is green and T36 is committed.
8. After W0.5/W0.6, run W1–W10 in dependency order (section 3 table). W2 and W3 may run in parallel after
   W1; W8/W9 are independent.

When in doubt, re-read `AGENTS.md` R1–R13 and `docs/CODING_STANDARDS.md`. The plan may be wrong; the rules
and the tests are not. **No shortcuts, no fallbacks, no cheat codes.**
