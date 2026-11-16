# Build plan: seven modules, each built whole by one agent

Status: plan (Nov 14, 2026). Nothing here is built.
This file turns the three architecture plans into seven modules:

- `docs/AGENT_INTERACTION_ARCH.md` (packages H1 to H11, spikes SI1 to SI5)
- `docs/REVIEW_ARCH.md` (packages RV1 to RV11, spikes SR1 to SR3)
- `docs/TEST_AGENT_ARCH.md` (packages TA1 to TA10, spikes ST1 to ST4)

**The idea.** One agent takes one module, reads its context once, and builds the whole thing from start to
finish. A module owns whole folders, so two agents never edit the same file. There are only two points where
work waits for other work. That is faster and safer than many small pieces handed to many agents, where each one
re-learns the context and the pieces have to be fitted together afterwards.

---

## 0. Builder tiers

| Tier | Gets |
|---|---|
| **high** | Core, security boundaries, authority, the gate, golden files, review and merge |
| **medium** | A whole feature module with a contract, tables of expected behaviour and golden files |
| **low** | Text, rules and small scripts with golden tests; no server code |

Which model sits in each tier is the owner's choice.

---

## 1. The seven modules at a glance

| Module | What it delivers | Builder | Starts |
|---|---|---|---|
| **M0 Core** | Shared files split by area; the state machine replaced by a fold, predicates and `Missing`; kinds and seats | **high** | now |
| **M1 Runner** | Everything the sandbox runs: checks, test kinds, the queue, databases and a served app, the browser preview, the history mirror | **high** | now |
| **M2 Content** | Lens packs and their pattern rules, stack and method skills, the `checks` image and its converters, test lint rules | **low** | now |
| **M3 Review** | Tool results, routing by lens, the new review shape, refute, scope and budget, the co-change miner, learning | **medium** | after M0's step 1 |
| **M4 Tests** | The test plan, staged files, test kinds, mutants and survivors, the critic, the UI handles | **medium** | after M0's step 1 |
| **M5 Interaction** | Threads and the status board, who a message is for, questions, quiet agents, the record, `propose_command`, the lead agent, the agent host | **medium** | after M0's step 1 |
| **M6 Proof** | Golden files up front; the spikes; merging; live checks on the VM; the benches and ablations | **high**, with the owner at the VM | now, and again at the end |

```
 Stage 1 (in parallel)      M0 Core        M1 Runner        M2 Content        M6: spikes + goldens
                               |               |                |
 Stage 2 (in parallel)      M3 Review ---> M4 Tests         M5 Interaction          (all need M0's step 1 merged;
                            (M4's last three steps need M3's first two)                  M0 keeps going beside them)
                               \               |               /
 Stage 3                              M6: merge, live checks, benches
```

Seven agents at most. M1 and M2 never wait for anyone. Stage 2 starts as soon as M0's first step (the split
of the shared files) is merged, not when all of M0 is done.

---

## 2. Who owns what

A module may create, edit and delete only inside its own folders. It may read anything.

| Module | Owns |
|---|---|
| M0 | `internal/archcheck/`, `scripts/check-owned.sh`, `e2e/task_m0_test.go`, `internal/domain/change/`, `internal/domain/workflow/`, `internal/domain/policy/`, `internal/app/bundle/`, `internal/app/people/`, `internal/app/intake/`, `internal/app/trigger/`, `internal/app/agenttools/`, `internal/app/dashboard/`, `internal/app/engine/` (step framework only), `internal/adapters/postgres/`, and the split of `mcp/tools.go` and `cmd/heimdall-server/app.go` into area files |
| M1 | `internal/adapters/rootlessrunner/`, `internal/adapters/localrunner/`, `internal/adapters/profileyaml/`, `internal/ports/types_profile*.go`, `internal/ports/sandbox.go`, `internal/ports/types_work.go`, `deploy/heimdall-runner.service`, `e2e/task_m1_test.go` |
| M2 | `deploy/lenses/`, `deploy/checks/`, `skills/` (text only, not `embed.go`) |
| M3 | `internal/domain/sarif/`, `internal/domain/rulecard/`, `internal/domain/finding/`, `internal/domain/present/`, `internal/app/review/`, `internal/app/reviewbrief/`, `internal/app/reviewlearn/`, `internal/app/codegraph/`, `internal/adapters/lensfs/`, `internal/mine/`, `cmd/heimdall-mine/`, and the review area files: `engine/steps_review*.go`, `effects/check*.go`, `effects/verify_evidence.go`, `mcp/tools_review.go`, `mcp/tools_checks.go`, `change/events_review.go`, `change/events_checks.go`, `workflow/rows_review.go`, `cmd/heimdall-server/wire_review.go`; test side: `cmd/heimdall-testagent/scenarios_review*.go`, `scenarios_verdict*.go`, `e2e/task_m3_test.go` |
| M4 | `internal/domain/testplan/`, `internal/domain/testset/`, `internal/domain/verify/`, `internal/domain/gate/`, and the tests area files: `engine/steps_tests*.go`, `engine/steps_commit.go`, `effects/test*.go`, `effects/acceptance.go`, `mcp/tools_tests.go`, `mcp/tools_files.go`, `mcp/tools_preview.go`, `change/events_tests.go`, `workflow/rows_tests.go`, `cmd/heimdall-server/wire_tests.go`; test side: `cmd/heimdall-testagent/scenarios.go` and its test, `e2e/task_m4_test.go` |
| M5 | `internal/app/announce/`, `internal/app/collaboration/`, `internal/app/roomstream/`, `internal/app/liveness/`, `internal/app/toolguard/`, `internal/app/record/`, `internal/app/questions/`, `internal/domain/commands/`, `internal/adapters/switch/`, `internal/ports/chat.go`, `internal/transport/skills/`, `internal/pack/`, `deploy/agents/`, `testdata/agents/`, `skills/embed.go`, and the talk area files: `mcp/tools_commands.go`, `mcp/list.go`, `change/events_talk.go`, `cmd/heimdall-server/wire_board.go`, `wire_liveness.go`, `nudge.go`, `command_consumer.go`; test side: `cmd/heimdall-testagent/scenarios_talk*.go` (new), `e2e/task_m5_test.go` |
| M6 | `testdata/plan/`, `testdata/real/`, `testdata/repos/`, `testdata/reviewbench/`, `testdata/testbench/`, `internal/testkit/`, `e2e/bench/`, `e2e/scenarios/`, `e2e/stories/`, `docs/PROGRESS.md` evidence, and every merge |

**Why this holds.** M0's first job is to split the few files everyone registers into (the event registry, the
1,000-line tools file, the 972-line wiring file, the workflow rows, the policy and profile structs) into one file
per area. After that each stage 2 module has its own `tools_<area>.go`, `events_<area>.go`, `rows_<area>.go`
and `wire_<area>.go`, listed above, and never opens another module's.

If a module needs a change in a folder it does not own, it does not make it. It writes the need in
`docs/PROGRESS.md` under "Noticed" and carries on with the rest. The M6 integrator settles it at the merge.

---

## 3. Rules for every module

These are the repository's rules (`CLAUDE.md` R1 to R13) and the construction rules C1 to C9 of
`AGENT_INTERACTION_ARCH.md`, plus what makes one-agent-one-module work.

1. **Read the whole brief and the plan sections it names before writing code.** The module is one piece of work.
   Do not stop between its steps.
2. **Work the steps in the order given.** Each step ends with its checks passing before the next begins.
3. **Tests first for real logic only** (R1): state rules, parsing, gates, authority, isolation. Text, rows and
   wiring get no new tests.
4. **Goldens are the truth.** Where a step names golden files, the code reproduces them exactly. A builder never
   edits a golden, a given type or a given signature. If one looks wrong, say so and continue with other steps.
5. **Replace means delete**, in the same step (C2). Each module lists what it removes.
6. **Checks, at every step:** the step's focused command, then `make fast`. At the end of the module:
   `make verify`.
7. **Done-when is checkable:** a command and its output, or a search that must be empty.
8. Functions under 60 lines, complexity under 12 (the linter enforces it). No new dependency without a high-tier
   builder's decision. No test double in production code.
9. **After two failed attempts at the same error:** write what was tried in `PROGRESS.md`, skip that step, finish
   the others (R12).
10. **One branch per module, one worktree.** Commit as `M3: <what>`, one commit per step.
11. **Evidence** in `PROGRESS.md` when the module ends: commands, exit codes, key lines, and what is **not
    verified** with the reason. Live behaviour on the VM is always "not verified" until M6 runs it.

---

## 4. The modules

Each brief lists: what to read, the steps in order with the package they come from, the hard parts, what is
deleted, and the done-when. The detailed design of every step is in the plan section named beside it.

### M0 Core (high)

**Read:** `AGENT_INTERACTION_ARCH.md` sections 1 and 3 (F1, F2, F3); `internal/domain/change/doc.go`,
`reducer.go`, `state.go`; TECH_SPEC 3.3, 3.4; `domain/workflow/definition.go`; `mcp/tools.go`;
`cmd/heimdall-server/app.go`.

| Step | Do | Prove |
|---|---|---|
| 1 | **Split the shared files by area**, changing no behaviour: the event registry into `events_<area>.go`; `mcp/tools.go` into `tools_<area>.go` with their handlers; `app.go` wiring into `wire_<area>.go`; workflow rows into `rows_<area>.go`; policy into one embedded struct per area. Areas: core, tests, review, checks, talk | `make verify` passes with no test changed |
| 2 | **Push this step alone and let M6 merge it at once.** Stage 2 modules start from it | merged |
| 3 | **The fold** (H1): `view.go`, `can.go`, `missing.go`, `anomaly.go` beside the old reducer | A table per predicate. Matches the goldens in `testdata/plan/view/`. The two Nov 7 sequences (inside the recorded KAN-22 log) fold with no error; the anomaly half waits on the owner defining an anomaly (view README Q2) |
| 4 | **Move every caller** from `Reduce` to predicates: `agenttools`, `intake`, `trigger`, `dashboard`, `postgres/rooms.go` (M5 moves `command_consumer.go` itself, using the predicates). While in `agenttools/tasks.go`, take the tests folder from the task's inputs instead of the `"tests/heimdall"` literal | Each package's tests pass; assertions on `ErrIllegalTransition` become assertions on the predicate's refusal |
| 5 | **Delete** `reducer.go`, `state.go`, their tests. Rewrite TECH_SPEC 3.3 | `grep -r "change.Reduce" internal` is empty in M0's folders |
| 6 | **Kinds** (H2): `workflow/kinds.go`; bundle and wake line read it; the role switches go | `grep -rn "case workflow.Role" internal/app/bundle internal/domain` is empty |
| 2b | **Two guards that make review mechanical.** `scripts/check-owned.sh <module>`: fails when a branch's diff touches a path outside that module's row in section 2. An `archcheck` rule: `go/parser`, `go/ast` and `go/token` may not be imported under `internal/app`, `internal/transport` or `cmd/heimdall-server` (it starts as a list of today's offenders that M3 and M4 empty) | A fixture for each guard: one diff inside, one outside; one allowed import, one refused |
| 7 | **Seats** (H2, F3): `Policy.Seats` with `role` and `lenses`; `Person.Agent` becomes `Seat`; fixture policies updated | Validation table: unknown agent, unknown lens, two people on a seat, an adopted lens no seat holds |

**Hard parts:** step 4 and 5. Every refusal in the 20-state table must land in the right predicate, and nothing
refused before may quietly become allowed. Steps 3 to 5 change existing assertions on purpose and need the
owner's yes (R2), asked once.

**Deletes:** the transition table, the states, the role switches in its folders, `people.<p>.agent`.

**Done when:** `make verify` passes; `grep -r "ErrIllegalTransition" internal` finds only what M5 still has to
move, and that list is in `PROGRESS.md`.

### M1 Runner (high)

**Read:** `REVIEW_ARCH.md` section 4; `TEST_AGENT_ARCH.md` sections 5, 7.3, 7.7 and 8;
`internal/adapters/rootlessrunner/doc.go` and every file in it; TECH_SPEC 12.2 and 12.2b; the results of spikes
ST1 and SR1.

| Step | Do | Prove |
|---|---|---|
| 1 | **Profile fields** (RV1, TA1): `checks`, `tests.kinds`, `tests.runs`, `paths.tests`, `services`, `serve`, `ready` | Validation tables: an image must be on the allowlist; a kind's `run` needs `{dir}`; `ready` must be loopback |
| 2 | **One job shape for checks and test kinds** (RV1, TA1): base at `/base`, head at `/work`, result at `/out` as SARIF or JUnit, bounded. The command comes only from a promoted profile | Argument tests as the package has today. `CheckGoVet` and the old job enum values are deleted |
| 3 | **The queue** (TA1, section 8): priority by job kind, wait inside a deadline, cancel on a new head | With a fake runtime: order, waiting, cancel |
| 4 | **Services and a served app** (TA6) | Argument tests |
| 5 | **The preview job** (TA9): app plus headless Chromium, one at a time, ended on submit, failure or idle; the debugging endpoint reachable only by the calling agent, by the mechanism ST1 found | Ownership and lifetime tests |
| 6 | **The history mirror** (RV10): fetched only by the warm job, read-only to checks | Argument tests |

**Hard parts:** all of it. The runner executes untrusted code. Its tests prove the arguments, not the isolation.
Steps 2, 4 and 5 are **not done** until the escape checks in the package's `doc.go` are re-run on the VM, which
M6 does with the owner. Step 5 does not start before ST1's result is recorded.

**Deletes:** `CheckGoVet`, `SandboxStatic`, `SandboxContract`, `SandboxMutation`; the single-purpose job paths
they fed.

**Done when:** `make verify` passes and the list of live checks still owed is in `PROGRESS.md`.

### M2 Content (low)

No Go server code. Everything here is text, rules and small scripts with golden tests, so it suits the low tier
and can be split by folder among several of them if wanted: one lens, one stack, one converter each.

**Read:** `REVIEW_ARCH.md` 4.2, 5.1, 5.2; `TEST_AGENT_ARCH.md` 5.2, 7.4; the existing `skills/*/SKILL.md` and
`deploy/cards/examples/`.

| Step | Do | Prove |
|---|---|---|
| 1 | **Lens packs** (RV3): `deploy/lenses/<lens>/` for correctness, security, data, infra, contract, operability, tests. Each card has `applies_to`, `check`, `proof` | A high-tier builder reviews the security and data packs |
| 2 | **Pattern rules** inside each lens, and the test lint rules of `TEST_AGENT_ARCH.md` 5.2, for the fixture languages | Each rule has one file it fires on and one it does not; the pattern engine's own test mode passes |
| 3 | **Skills**: one page each for `review-method`, `test-method`, `ui-testing`, `lead`; stack skills from one fixed outline (pitfalls, how to write a test, how to read the seed) | The existing skill test passes: a skill names only real tools |
| 4 | **The `checks` image** (RV2): only tools that passed spike SR1, pinned by version and checksum | It builds; a self-test runs every tool offline |
| 5 | **Converters to SARIF** for tools that do not write it, including mutant reports (TA4) | Each reproduces its golden from a recorded real output |

**Hard parts:** none in the building. The judgement is in the wording of security and data cards, which is why a
high-tier builder reviews those.

**Done when:** every rule test and every converter golden passes, and the image self-test passes offline.

### M3 Review (medium)

**Read:** `REVIEW_ARCH.md` in full; `internal/app/reviewbrief/doc.go`, `internal/app/review/doc.go`,
`internal/domain/rulecard/`, `internal/app/codegraph/doc.go`.

| Step | Do | Prove |
|---|---|---|
| 1 | **Tool results** (RV1): `internal/domain/sarif/` with `Parse`, `Result`, `New(base, head)` | Matches `testdata/plan/sarif/`; a fuzz target never panics |
| 2 | **The check effect** (RV1): run each matching check on base and head, keep what is new, file a finding for an enforced rule, make the rest leads; `run_check(check_id)` | With a fake sandbox: selection, new-only, both routes |
| 3 | **Card scope and lens packs from the repo** (RV3): `Scope` and `Lens` on cards; `lensfs` reads `.heimdall/lenses/` at a SHA | Scopes rank alike except E3 (person and confirmed only); traversal and oversize refused |
| 4 | **Routing** (RV5): needed lenses from the diff and the results; seats from policy; lenses recorded in `reviewers.picked`; `leads` and `skills` in the brief | Needed-lens and seat-selection tables |
| 5 | **`Review@2` and the submit rules** (RV5): every obligation and lead answered; answers and findings agree | Matches `testdata/plan/review/` |
| 6 | **Refute** (RV7): the kind, the location rule, "is refuted" derived from facts | Location rule; later evidence reopens; a reporter cannot refute |
| 7 | **Scope and budget** (RV8) | Scope from a diff and the graph; ordering and the cap |
| 8 | **The co-change miner** (RV10): `internal/mine/`, `cmd/heimdall-mine/`, SARIF out; gaming signals for M4 live here too | Matches `testdata/plan/mine/` |
| 9 | **Learning extensions** (RV11): weights for lens cards and check rules, the refutation record, dismissal categories | Table tests |
| 10 | **The symbol index** (RV4), only if spike SR2 kept the bench cases | The bench cases G1 and G2 still pass, then the Go parser path and the regex taggers are deleted |

**Hard parts:** step 5. It decides what a valid review is. A high-tier builder reviews that step before the module
goes on.

**Push steps 1 and 2 first** and let M6 merge them at once: M4 builds on them.

**Deletes:** the old `run_check` enum, `domain/verify`-style tool judging in its area, `Review@1`, the
"answer it next time" rule for unanswered cards, the reviewer skill's pack text.

**Done when:** `make verify` passes; a recorded KAN-23 review, replayed through the new submit rules, is
accepted or refused for the reasons a person would give.

### M4 Tests (medium)

**Read:** `TEST_AGENT_ARCH.md` in full; `internal/domain/testset/doc.go`, `internal/domain/verify/doc.go`,
`internal/app/effects/test_quality.go`, `internal/domain/gate/doc.go`.

| Step | Do | Prove |
|---|---|---|
| 1 | **The tests folder from the profile** (TA1): one accessor; every literal in M4's folders goes | No `"tests/heimdall` literal in M4's folders |
| 2 | **Test kinds and repeated runs** (TA1): acceptance and red-first both run a kind `tests.runs` times and apply the stability rule | With a fake sandbox |
| 3 | **The test plan** (TA2): `internal/domain/testplan/`, its tool, sign-off on the plan | Matches `testdata/plan/testplan/`; fuzz decode |
| 4 | **Staged files and `TestSubmission@2`** (TA2): `put_file`; behaviours map to tests both ways | Path and size rules; mapping both ways |
| 5 | **Test lints through the check** (TA3): the Go-only checks are deleted | `grep -rn "go/parser" internal/app/effects` is empty |
| 6 | **Survivors** (TA4): the three answers, the kill check, the predicate "every survivor answered" | Predicate table; a new head resets it |
| 7 | **The gate row** (TA4): the merge rule needs every survivor answered; `tests.mutation_blocks` goes | The gate's table test gains the row and flips it |
| 8 | **The seed** (TA5): from the ticket on base, from the SHA on a head | Table test |
| 9 | **The critic** (TA8): evidence `counter_impl`, the patch rules, the run | Patch rules; all tests pass means E1 |
| 10 | **UI handles and `start_preview`** (TA9): handles in the plan, the tool that asks M1's preview job | The handle rule |
| 11 | **The test commit message** (H11) | A golden message |
| 12 | **Test knowledge in the brief** (TA10): seeds, flaky patterns, the heat map | A seed from a real fix commit gives a test that fails on the parent and passes on the fix |

**Hard parts:** step 7 changes the merge gate. A high-tier builder writes or reviews that one file. Step 9's patch
rules must make it impossible for a patch to touch the tests.

**Order note:** steps 5, 6 and 7 read tool results (test lints, surviving mutants) that M3's steps 1 and 2
produce. Do every other step first, then rebase onto M3's merged steps 1 and 2 and finish these three.

**Deletes:** the tests-folder literals, `placeholderReasons`, `verify/seam.go`, `TestSubmission@1`, the author's
own criteria map as input, `tests.mutation_blocks`, the single acceptance run, Go text in the test skill.

**Done when:** `make verify` passes; on a fixture, with the scripted agent, a plan with no negative case is
refused naming the criterion, and a weak suite's survivors block the gate until answered.

### M5 Interaction (medium)

**Read:** `AGENT_INTERACTION_ARCH.md` in full; `internal/app/announce/`, `internal/app/collaboration/`,
`cmd/heimdall-server/nudge.go` and `command_consumer.go`, `internal/adapters/switch/`, `deploy/agents/`; the
results of spikes SI1, SI2, SI3.

| Step | Do | Prove |
|---|---|---|
| 1 | **Every role works in the ticket's working room** (H4): `RoleRooms` and `roomFor`'s special case go | Existing tests, updated where the room changed on purpose |
| 2 | **Threads in the chat port** (H4, H7): `Post` with a thread, `Update` if SI1 found it | Contract test against recorded Switch payloads |
| 3 | **Who and what a message is for** (H4): the ticket from the thread's root or the key in the text; the addressee | Matches `testdata/plan/address/` |
| 4 | **The board** (H7): `Render(view, missing)`, one status message per change as the thread's root, action cards; the line table replaces the 18 narrators | Matches `testdata/plan/status/` |
| 5 | **Question timing** (H8): remind, escalate, assume, as one pure function | Table test |
| 6 | **Quiet agents** (H6): the pure `Next`, the provider probe, the per-agent clock, the talk budget | A table with every row of section 6.3 |
| 7 | **Tool reliability** (H5): the three-part refusal for every tool error, repeat detection, tool-call text as a stuck signal | Three equal refusals, then the changed message, then `stuck`; the real KAN-18 sample |
| 8 | **The record** (H8): every tool call, redacted; `heimdall trace show`, `heimdall show` | Redaction with planted strings; component test on real Postgres (migration `0013`) |
| 9 | **Verb classes and `propose_command`** (H10): the actor is the trigger message's sender; deciding verbs wait for that sender's `yes`; `command_consumer.go` moves to M0's predicates | The class table; the open-proposal predicate; the injection cases with the scripted agent |
| 10 | **The agent host** (H3): one template, the eight-line `AGENTS.md`, the skills catalogue route, subagent definitions, tools offered by active kind, Playwright's tools allowed or denied by name | Two agents render identical files apart from the token line; `AGENTS.md` names no missing tool |
| 11 | **The lead agent** (H10): manifest entry and its skill from M2 | Wiring only |
| 12 | **Room post scan** (H11) | Planted token, canary and card text are each caught |
| 13 | **Remove the pack pipeline** (H9): move the scanner cards use to its own package first | `grep -rn "PackExists\|read_pack\|publish_pack" internal cmd skills` is empty |

**Hard parts:** step 9. It decides whose authority a command runs under. A high-tier builder writes or reviews
`propose_command` before the module goes on. Steps 1, 2 and 10 depend on spike results and wait for them; the
other steps do not.

**Deletes:** `RoleRooms`, the 18 narrator functions, the skills install step, the five per-role templates, the
old `AGENTS.md` rules, the pack pipeline, `TestQualityDeps.Code` and `effects.BaseReader` if M4 has not already.

**Done when:** `make verify` passes; with the scripted agent, "dismiss F2" proposed for a mapped sender is held
until that sender's `yes`, and the same text with no trigger message changes nothing.

### M6 Proof (high, with the owner)

M6 is the only module that runs at both ends.

**At the start, in parallel with M0, M1 and M2:**

| Step | Do |
|---|---|
| 1 | **The spikes**, on the VM with the owner: SI1 to SI5, SR1 to SR3, ST1 to ST4. Each result goes into `PROGRESS.md`. A module step that names a spike waits for it |
| 2 | **The goldens**, in `testdata/plan/`: the view and `Missing` for the recorded KAN-22 and KAN-23 logs at five points each; the two Nov 7 sequences with the expected anomaly; the status text for the same points; SARIF pairs and their new-only result; valid and refused test plans; valid and refused reviews; co-change histories and their rules; chat messages with their ticket and addressee |
| 3 | **Bench cases and gold answers** (`testdata/reviewbench/`, `testdata/testbench/`), written before any run, with a held-out set |
| 3a | **The baseline.** Run `make verify` on `heimdall-build` before any module starts and record the exit code, the test count and any failing test in `PROGRESS.md`. Every later result is read against it |
| 3b | **Two more fixture repositories** in `testdata/repos/`, each with known red and green tests and a profile: a Python service that needs a database, and a small web app with one page and one form. Today both fixtures are Go, so nothing can show that the server is language-neutral or that a UI test runs |
| 3c | **Recorded real payloads** in `testdata/real/<system>/`, each with a README on how it was captured, scrubbed: a Switch thread post and a thread read; the SARIF or raw output of every tool that passed SR1; JUnit from pytest and from Playwright; one mutation report per language. The folder does not exist today, so the contract layer has nothing to compare against. Capture them during the spikes |
| 3d | **Where module branches live.** `make fast` walks every `.go` file under the repository, including `.claude/worktrees/`, and fails there on the deliberately bad lint fixture (noted Nov 7). Either the owner adds that exclusion to the `Makefile` first, or every module's worktree is a sibling folder outside the repository. Decide once, before stage 1 |

**At the end:**

| Step | Do |
|---|---|
| 4 | **Review each module** against the list in section 6, the flagged hard parts first |
| 5 | **Merge** in this order: M0, M1, M2, then M3, M4, M5. Run `make verify` after each. Settle every "Noticed" cross-module need |
| 6 | **Live checks** on the VM: the runner's escape checks; one ticket end to end on each chat platform; every "done when" in the three plans that says "live" |
| 7 | **Benches and the ablation table** (RV9, both benches), on the production model, cost stated first, under the $3 cap |

**Hard parts:** a wrong golden makes a cheaper builder faithfully build the wrong thing. Gold answers must come
from someone who will not also tune the skills against them. Live results need honest "not verified" lines.

---

## 5. Testing and validation

### 5.1 What was missing from this plan

Checked against `docs/TEST_STRATEGY.md`, `docs/CODING_STANDARDS.md` section 8 and the code on Nov 15.

| # | Gap | Evidence | Fixed by |
|---|---|---|---|
| V1 | The plan said "tests first, `make fast`, `make verify`" and nothing about which test layer proves what | TEST_STRATEGY section 2 defines L0 to L7 | 5.2 |
| V2 | The scripted agent and the task checks had no owner. Three modules change what agents submit; the scripted agent (`cmd/heimdall-testagent`) speaks the old shapes and would silently stop matching | section 2 had no row for it | section 2, now |
| V3 | Only one `TestTask_*` exists in `e2e/`. `make e2e T=<id>` fails for a task with no such test, so a module had no real-system check to run | `grep -rn "func TestTask_" e2e` gives 1 | each module owns `e2e/task_m<n>_test.go` |
| V4 | Both fixture repositories are Go. "Any language" and "UI tests" cannot be shown | `testdata/repos/`: `pay-demo`, `ledger-demo` | M6 step 3b |
| V5 | `testdata/real/` does not exist. Steps that said "contract test against recorded payloads" had nothing to stand on | `ls testdata/real` | M6 step 3c |
| V6 | No baseline. A module could not tell its own failure from one that was already there | the Nov 8 notes list earlier race failures | M6 step 3a |
| V7 | Worktrees inside the repository break `make fast` | `Makefile` `GOFILES`; PROGRESS Nov 7 | M6 step 3d |
| V8 | "The diff stays in the module's folders" and "no language in server code" depended on a reviewer noticing | section 5 list | M0 step 2b |
| V9 | No adversarial pass, which the repo's own loop requires (TEST_STRATEGY 5, step 6) | absent | 5.4 |
| V10 | New packages had no design note. The repo's loop starts every package with a `doc.go` | TEST_STRATEGY 5, step 1 | 5.3 |
| V11 | Changes to what a model sees (skills, briefs, tool shapes) had no live smoke assigned, and new untrusted inputs had no injection case | TEST_STRATEGY L5, L6 | 5.2 |
| V12 | TECH_SPEC 14.1's tool table feeds a schema test. A module that adds a tool and not its row turns that test red for everyone | `internal/schema` | 5.3 |

### 5.2 Which layer proves each module

Layers as in TEST_STRATEGY section 2. "Builder" runs it before handing over. "M6" runs it at the merge or on
the VM.

| Module | L1 unit, table, fuzz (builder) | L2 component (builder) | L3 contract (builder) | L4 real systems, scripted agent (M6) | L5 live model (M6) |
|---|---|---|---|---|---|
| M0 | every predicate; `Fold` against goldens; a replay property | the store's stream tests on real Postgres, unchanged | recorded event logs fold with no error | `TestTask_M0`: one ticket to merged on the fixture, no anomaly | none: no model-facing change |
| M1 | argument construction; queue order; profile validation | each job kind on all four fixture repositories with the real runner | JUnit and SARIF from `testdata/real/` parse | `TestTask_M1` on the VM: the escape checks, a database fixture with no egress, a preview reached only by its agent | none |
| M2 | every rule fires and does not fire; every converter matches its golden | the image self-test, offline | converters run on `testdata/real/` outputs | none | one smoke per skill at the end, with M3 to M5 |
| M3 | SARIF, routing, submit rules, refute, scope, miner | the check effect with the real runner on a fixture; `lensfs` at a real SHA | `Review@2` and the tool schemas as golden JSON | `TestTask_M3`: the scripted agent files a review with leads, a refutation, a refused review | one review per seat on the fixture PR; the injection suite with the new inputs |
| M4 | plan, mapping, survivors, the gate row, patch rules, seed | red-first and repeated runs with the real runner on the Go and the Python fixture | `TestPlan@1`, `TestSubmission@2` as golden JSON | `TestTask_M4`: plan, staged files, a rejected plan, survivors that block and then clear | one ticket per fixture language; the injection suite |
| M5 | address, timing, liveness, tool guard, verb classes, the open-proposal rule | the record on real Postgres; the board against a fake chat port | Switch thread post and read against `testdata/real/switch/` | `TestTask_M5` on the VM's Switch: one status message kept current, a thread reply, a held command confirmed by its sender and refused from anyone else | "why is it blocked?" and "dismiss F2" to the lead; the injection suite |

Rules that go with the table:

- A module is **built** when its L1 to L3 pass. It is **done** only when its L4 has run. L5 is recorded with its
  cost, stated before the run, under the $3 cap.
- A layer that could not run is written `not verified` with the reason. It is never replaced by a lower layer.
- The scripted agent lives only in test code (D121). A module that changes a submit shape changes the scripted
  agent's scenario in the same step, so L4 keeps meaning something.

### 5.3 What every kind of code must be tested for

A builder picks the row that fits each thing it writes. These are the etiquette, in checkable form.

| Kind of code | Must have |
|---|---|
| **A new package** | A `doc.go` first, from `docs/harness/doc.go.template`: purpose, API, invariants, failure modes, tests |
| **A pure rule or predicate** | A table test with one row per allowed case and one per refusal, each refusal checked with `errors.Is`. For the gate: the row that flips it |
| **A parser or decoder** (SARIF, the plan, a message's address) | Bounds on size and depth; malformed input refused with the field named; unknown fields per the format's rule; a fuzz target that never panics; a round trip where the type encodes |
| **An effect** | With fakes for every port: it performs the action once; a retry after a crash repeats nothing; an error's text is fixed and carries no cause text (the canary test the package has today); it fails closed |
| **A fold or derived view** | Golden views from recorded logs; any prefix of a log folds; the same events in the same order give the same view |
| **A tool** (MCP) | Its schema as golden JSON; allowed only for its kind; every refusal has field, problem and example; its row in TECH_SPEC 14.1 added in the same step |
| **A runner job** | Argument construction tests, a refusal for every forbidden input (image off the list, a path that escapes, oversize), and a live escape check listed as owed |
| **An adapter** | Real Postgres or the real runner in a component test; a contract test against a recording in `testdata/real/` |
| **A skill, a card, a prompt, a row of text** | No new test (R1). The existing check that a skill names only real tools still passes. Its proof is the live smoke |

For every test: it fails first on an assertion, never on a compile error or a missing file; it uses no network,
no real clock and no shared state; it is never retried until green; fakes exist only for ports in the
application layer's unit tests.

### 5.4 The adversarial pass

Once per module, by the builder, before handing over; then again by the reviewer in a fresh session, because
the author is the worst person to find their own gaps. Each question that applies gets a test. Each that does
not gets one line saying why.

| Question | Example in these modules |
|---|---|
| Invalid input | a plan with 10,000 behaviours; a SARIF file with a path that escapes |
| Replay | the same submit twice; the same `yes` twice |
| Two things at once | two seats refute the same finding; a push arrives during a check run |
| A dependency is down | the runner refuses; Switch is unreachable; the model provider refuses |
| The wrong person | an agent proposes a command with another person's message as trigger; an unmapped sender says `yes` |
| A stale SHA | a `yes` after a new head; a refutation that cites the old head |
| Oversized | a 10 MB tool output; a 1 MB chat message |

## 6. Review and merge

One reviewer, a high-tier builder, for every module. In this order, sending the module back at the first failure:

1. `scripts/check-owned.sh <module>` passes: the diff stays inside the module's folders.
2. No test was skipped, deleted or loosened, and no golden changed (R2).
3. The module's flagged hard parts, read line by line.
4. `make verify` exits 0 on the branch, and the result is read against the baseline.
4a. The module's rows in 5.2 and 5.3 are covered, and its adversarial pass (5.4) is written down.
5. The construction rules: one path, the replaced thing deleted, no stored state a fold should compute, no
   language or tool name in server code.
6. The evidence in `PROGRESS.md` matches what the reviewer saw.

**What needs the owner's yes** (R2), asked once per stage: the changed assertions when the state machine goes
(M0); new recorded tool outputs under `testdata/real/` (M2); new board steps in `e2e/scenarios` and
`e2e/stories` for the live checks (M6); any change to `.golangci.yml`.

---

## 7. The brief an agent receives

```
MODULE <M#>: <name>
You build this whole module in one go. Do not stop between steps.
Read first: <the plan sections and files from the module's "Read" line>
You own (change only these): <the module's row in section 2>
If you need a change anywhere else: do not make it. Write it under "Noticed" in docs/PROGRESS.md and carry on.
Steps, in order: <the module's table>
For each step: write the named tests first where the step has real logic, see them fail once, build, then run
  the step's focused test and `make fast`.
Never edit a golden file, a given type or a given signature.
Delete what you replace, in the same step.
If the same error survives two attempts: note it in docs/PROGRESS.md, skip that step, finish the others.
Follow section 5: the layer table row for your module, the checklist for each kind of code, one adversarial pass.
At the end: `make verify`, then write the evidence: commands, exit codes, key lines, what is not verified and why.
```

---

## 8. What this plan does not decide

- Which concrete model sits in each tier. A module marked medium or low can be given to a higher tier. A module
  marked high should not be given to a lower one.
- Costs of builder runs. Live product runs keep the $3 testing cap.
- Anything the spikes have not answered.

**Not verified:** nothing here has been tried. The ownership table comes from reading the code on Nov 14. The
"Noticed" rule in section 2 is what happens when it turns out to be wrong somewhere.
