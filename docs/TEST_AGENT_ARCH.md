# Test agent architecture: tests that are evidence, in any language, including the UI

Status: architecture and plan, third pass (Nov 14, 2026). Nothing here is implemented.

Who builds each package, in what order, and how each piece is proven: `docs/BUILD_PLAN.md`.

Read `docs/AGENT_INTERACTION_ARCH.md` first: its construction rules C1 to C9 and foundations F1 to F6 are assumed
here. `docs/REVIEW_ARCH.md` section 4 defines checks (tool runs declared in the repo profile, SARIF out), which
this plan reuses. `docs/KNOWLEDGE_TOPOLOGY.md` section 4 defines the test knowledge (regression seeds, flaky
memory, heat map) that the last phase brings in.

Labels: **verified in code** = read in this repository on Nov 14. **research (F)** = a helper fetched the primary
page. **research (S)** = search summary only. **not verified** = nobody checked; each is behind a named spike.

---

## 0. The one-sentence logic

**A test is evidence only if it was derived from the spec, can fail for a wrong implementation, and gives the
same answer every run. The author can only promise the first. Only the harness can prove the other two, and it
can prove the second only once an implementation exists. So there are two moments of truth: before the code
(shape, oracle, a real red) and after the code (mutants, varied inputs, counter-implementations).**

The test side uses the same four mechanisms as the review side and adds one:

| Mechanism | Used for |
|---|---|
| **Test kind** (a row in the profile, JUnit out) | How a kind of test is run: API, property, contract, UI |
| **Check** (a row in the profile, SARIF out) | Test lints, surviving mutants, gaming signals, accessibility |
| **Card** and **skill** | What the author must cover (regression seeds, flaky patterns) and how to work (the test method, a language's test notes, UI testing) |
| **Evidence tier** | A counter-implementation or a failing script is E1 |
| **Preview** (new) | A running copy of the app with a browser, inside the sandbox, that an agent drives with Playwright's own MCP server |

---

## 1. Where we are (verified in code)

| Area | Today | Weakness |
|---|---|---|
| Author isolation | The author reads the base commit only. The server commits and pins the tests by tree hash | Good. Keep |
| Red-first | Tests run on base; a pass or a compile error rejects (`effects/test_quality.go`) | For a new route any assertion is red, because the route answers 404. It proves the test can fail, not that it can tell right from wrong |
| Stability | 3 runs on base | On base everything fails the same way, so it measures almost nothing. The run on the PR head (`effects/acceptance.go`) is a single run |
| Seam | A Go import check (`verify/seam.go`) and a Go "placeholder" check that parses imports with `go/parser` | Go only, in server code |
| Criteria coverage | The author's own map from criterion to test names | Nothing checks what a test asserts |
| Oracle strength | Nothing | `status == 200` alone passes everything |
| Mutation | Enum value, policy fields and a workflow row. No job | Not built. The skill's `tests_too_weak` section is never triggered |
| Services | Specified in TECH_SPEC 12.2b. No field in `ports/types_profile.go`; every job has `--network=none` | A repo that needs a database cannot run its tests |
| UI | Nothing | No browser anywhere |
| Language | The skill, the package-name rule, `httptest`, the checks | Go assumed throughout |
| Tests folder | `"tests/heimdall"` written in 8 non-test places; policy has `tests.dir`, the profile has `paths.tests` | One fact, three homes |
| Submit | One call carrying every file as escaped text | KAN-18: the model looped on this call and filled 1.4M tokens |
| Capacity | 2 vCPU, 16 GB; a job gets 2 CPU, 2 GB, 512 MB work space; reviewers were refused with "runner is busy" | No queue |

---

## 2. What the research says

| Finding | Number | Trust |
|---|---|---|
| Tests generated after seeing faulty code find fewer faults than tests written independently | 14% vs 25% | research (F), arXiv 2607.05139 |
| Generators that aim for green produce suites that validate the bug | up to 68.1% of final suites passed on buggy code and failed on the reference | research (F), arXiv 2412.14137 |
| Mutation-guided test generation at Meta | 73% of tests accepted by engineers; an LLM spots equivalent mutants at precision 0.95, recall 0.96 after simple pre-processing | research (F), arXiv 2501.12862 |
| Google runs mutation on changed lines only, capped per review | method | research (S) |
| Agents game visible tests (special-casing, editing tests). "Do not cheat" barely helps. Hiding tests removes most of it | RE-Bench 30.4% of runs | research (F), METR Jun 2025; ImpossibleBench (abstract) |
| Agentic property-based testing with reflection | 56% of sampled reports valid, 86% of the top-ranked | research (F), vendor data, Anthropic Jan 2026 |
| Most common smells in LLM tests: assertion roulette, magic numbers | no percentages | research (F), arXiv 2410.10628 |
| LLMs judge flakiness inconsistently | qualitative | research (S) |
| Diff-scoped mutation exists per language: `cargo-mutants --in-diff`, `go-mutesting --git-diff-lines`, Infection `--git-diff-lines`; Stryker has a JSON report schema a few tools share | no universal tool or format | research (S) |
| Playwright emits JUnit (`--reporter=junit`) | | research (F) |
| Playwright's Docker guide: the image runs browsers as root with Chromium's sandbox off; for untrusted pages use a non-root user with the seccomp profile they publish | | research (F), playwright.dev/docs/docker |
| Playwright MCP (Microsoft, open source) drives a browser through MCP tools, returns accessibility snapshots by default, and can attach to an existing browser with `--cdp-endpoint`. Its `--allowed-origins` is documented as "not a security boundary" | | research (F) |
| OpenCode 2.x has a built-in `browser` tool namespace. It is not documented beyond a permission rule. A third-party report on 2.1.4 says it fails with `browser.disconnected` unless a desktop browser is attached | | research (F) for the permission page; third-party for the behaviour |
| Schemathesis fuzzes a running API from its OpenAPI file and writes JUnit | `--report junit` | research (S) |

What follows:

1. Spec-first tests by an author who never sees the implementation is the choice with the best measured support.
   Never trade it away.
2. Red-first is the weak half of fail-to-pass. The strong half is failing on a wrong implementation: mutants and
   counter-implementations.
3. Prompts do not stop gaming. Structure does.
4. Flakiness is decided by reruns on the passing path.
5. No language owns testing: every runner writes JUnit, every analyser can be made to write SARIF. The harness
   needs only those.
6. Nobody has published a proven recipe for agent-written UI tests. Section 7 is designed from first principles
   and sits behind a spike.

---

## 3. Principles

1. **The author promises, the harness proves.** A rule that lives only in the skill is a hope.
2. **Spec-first, always.** A test added after the implementation exists must trace to a criterion, or it becomes
   a question to a person.
3. **Every behaviour declares its oracle.** "No oracle" is refused by the server.
4. **Exploration never gates. Files gate.** A model may look at a running app. Only a re-runnable test file can
   turn a check red.
5. **Untrusted code, and any browser that renders it, runs only in the sandbox, with no network.**
6. **The harness knows JUnit and SARIF.** Languages, runners and tools are rows in the profile.
7. **One path.** If a repo's profile does not declare a kind of test, the plan refuses behaviours of that kind
   and the criterion goes to a person. Nothing weaker is silently accepted.

---

## 4. The test plan

### 4.1 What it is

Before any test code, the author submits `TestPlan@1`: what the tests will check, as data.

```json
{
  "ticket": "KAN-31",
  "behaviours": [
    {"id": "B1", "ac": "AC1", "kind": "api", "case": "positive",
     "given": "a captured payment of 100", "when": "POST /refunds amount=40", "then": "201 and remaining=60",
     "oracle": "exact"},
    {"id": "B2", "ac": "AC1", "kind": "property", "case": "rule",
     "given": "any sequence of partial refunds", "then": "the sum of refunds never exceeds the payment",
     "oracle": "property"},
    {"id": "B3", "ac": "AC2", "kind": "api", "case": "negative",
     "given": "another merchant's payment", "when": "POST /refunds", "then": "404 and nothing stored",
     "oracle": "exact"}
  ],
  "ui_handles": [],
  "expected_paths": ["internal/refunds/**", "api/openapi.yaml"],
  "assumptions": ["Refunds are in the payment's currency."]
}
```

### 4.2 Server checks on the plan (pure)

- Every criterion has a behaviour, and at least one `negative` or `boundary` behaviour.
- Every behaviour's oracle is `exact`, `property`, `metamorphic`, `differential` or `regression_seed`.
- A behaviour with `case: rule` has oracle `property` or `metamorphic`.
- Every `kind` is declared in the profile (section 5). An undeclared kind is refused with the declared ones
  listed.
- Sizes are bounded.

### 4.3 What it buys

| Benefit | Why |
|---|---|
| A cheap human checkpoint | A person reads six lines of given/when/then faster than 200 lines of code. In sensitive domains the sign-off is on the plan. Code then checks the tests against the plan |
| A contract for the files | Every behaviour has a test and every test has a behaviour, checked on submit |
| Small calls | The plan is one small object; files are staged one at a time with `put_file` (`AGENT_INTERACTION_ARCH.md` 6.2) |
| The developer's handover | The status and the PR comment list the behaviours, the expected paths and the UI handles |
| Input for the critic, the reviewers and the commit message | They read the plan |

It fixes the output's shape and leaves the reasoning to the model (D122).

---

## 5. Test kinds and checks come from the profile

### 5.1 The profile

```yaml
# .heimdall/profile.yaml
paths: {tests: tests/heimdall}        # the one place the tests folder is named
tests:
  runs: 3                             # every gate run is repeated; a result that changes is flaky
  kinds:
    api:      {run: "pytest {dir} --junitxml=/out/junit.xml"}
    property: {run: "pytest {dir} -p hypothesis --hypothesis-seed=$HEIMDALL_SEED --junitxml=/out/junit.xml"}
    contract: {image: checks, serve: "uvicorn backend.main:app --port 8000", ready: "http://127.0.0.1:8000/healthz",
               run: "schemathesis run api/openapi.yaml --url http://127.0.0.1:8000 --report junit --report-junit-path /out/junit.xml"}
    ui:       {image: browser, serve: "...", ready: "http://127.0.0.1:8000/",
               run: "npx playwright test {dir} --reporter=junit"}
services:
  - {name: db, image: postgres, env: {POSTGRES_PASSWORD: test-only}}
checks:
  - {id: test-lints, lens: tests, when: ["tests/heimdall/**"], image: checks,
     run: "opengrep scan --config .heimdall/rules/tests --sarif-output /out/result.sarif /work/tests/heimdall", baseline: false}
  - {id: mutants, lens: tests, when: ["**/*.py"], image: python,
     run: "mutants-sarif --diff /base /work --tests {dir} > /out/result.sarif", baseline: false}
```

- A **test kind** names how to run tests of that kind. Its result is JUnit. `{dir}` is the ticket's folder
  under `paths.tests`.
- A **check** is as in `REVIEW_ARCH.md` section 4. Its result is SARIF.
- `serve` and `ready` start the app inside the job and wait for a loopback URL.
- `services` are throwaway containers on a private network with no egress (TECH_SPEC 12.2b, D153; specified,
  not built).

The server has one executor for test kinds, one for checks, and names no language. The eight `"tests/heimdall"`
literals are replaced by the profile's value.

### 5.2 Test lints are pattern rules, not Go code

The `tests` lens pack (`REVIEW_ARCH.md` 5.2) carries small pattern rules per stack. They run as the `test-lints`
check over the submitted files, and each result is a refusal reason with file and line.

| Rule | Catches |
|---|---|
| no-skip | a skipped or expected-failure test, or a build tag that hides it |
| no-sleep | waiting on the clock |
| no-outside-network | a call to a literal host other than the app under test |
| can-fail | a test with no assertion |
| named-cases | table rows or parameters without names |
| seam-only | an import the repo's seam rule forbids (generated at setup from the profile's seam) |
| reaches-the-product | a test file that imports nothing of the project |
| locator-by-handle | in UI tests, a raw CSS or XPath locator (section 7) |

Adding a language is adding rule files. The Go parser checks in `effects/test_quality.go` and
`domain/verify/seam.go` are deleted. Whatever needs judgement (is this assertion meaningful?) is not a lint: it
is measured by mutants or judged by the critic.

### 5.3 Property tests and the seed

The harness sets `HEIMDALL_SEED` for every run: derived from the ticket key on base, from the head SHA on a PR
head. A run is reproducible from the SHA, and every push sees different inputs, so an implementation that
special-cases the inputs it saw fails the next push. Stack skills say how each language's library reads a seed.

---

## 6. The verification ladder

Cheapest first. A failing rung returns every reason found at that rung.

### 6.1 Before the implementation exists

| Rung | Check | Status |
|---|---|---|
| V0 | The plan is valid (4.2). Files map to behaviours both ways | new, pure |
| V1 | `test-lints` check on the staged files | new, replaces the Go checks |
| V2 | Red on base: every test fails with a JUnit `<failure>`, never an `<error>` | exists |
| V3 | The same result on each of `tests.runs` runs | exists, generalised |

Before the implementation exists nothing can measure whether these tests would catch a wrong one. V0 to V3
prove shape, intent and determinism, and no more. The document says so and the status never claims otherwise.

### 6.2 On every PR head

| Rung | Check | Result |
|---|---|---|
| V4 | The acceptance run, `tests.runs` times. A test whose result changes is quarantined and named | gate, as today but stable |
| V5 | The `mutants` check: mutate only lines the change added or modified, run only the ticket's tests, capped by policy (start: 40 mutants, 600 s). Each surviving mutant is one SARIF result | 6.3 |
| V6 | Gaming signals, a check over the diff: a literal that occurs in the test inputs now occurs in product code; product code reads `HEIMDALL_SEED` or detects a test run; the change touches `paths.tests` | enforced E2 findings |
| V7 | The `contract` kind, when declared | failing cases are E2 findings |

Mutation tools differ per language and none is universal. That is why `mutants` is a profile row whose command
ends in a converter to SARIF: the server sees surviving mutants as results and knows no tool. Which tool per
language, and its diff mode, is spike ST2.

### 6.3 What a surviving mutant means

"This change to the code is invisible to the tests." There are three truthful answers. The author gets a
`survivors` task with the list and answers each:

| Answer | The author submits | The server checks |
|---|---|---|
| The tests are weak | A new or tightened test under an existing criterion | It fails on the mutant, passes on head, passes V1 |
| The mutant is equivalent | `equivalent` and one sentence | Recorded and shown in the review brief. A seat may file against it; an overturned claim counts against the author's record |
| The spec is silent | `unspecified`: no criterion decides this behaviour | A question to the ticket's assignee, in plain words. The answer becomes a criterion change or "does not matter", recorded |

The third answer is the important one. The mutant shows the author a line of the implementation. Tests written
from an implementation validate its bugs. So a strengthening test may not invent expected behaviour from the
code: if no criterion decides, a person does.

The gate needs every survivor answered. It needs no score. A threshold invites equivalent-claim spam and
arguments about a number. An answered list can be audited. `min_mutation_score` stays as a displayed target.

### 6.4 The critic: a counter-implementation as evidence

Mutants find small holes near the written code. They do not find a wrong reading of the spec. Before the
developer builds, one seat with the `tests` lens gets a `critique` task:

- It sees the criteria, the plan and the tests, not the author's notes.
- Its job is to break the suite: the smallest patch against base that makes every test pass and still violates a
  criterion.
- It files a finding of category `tests` with evidence type `counter_impl`: the patch (staged with `put_file`)
  and the criterion it violates.
- The harness applies the patch to base and runs the tests. If they all pass, half the claim is proven by code:
  this suite admits this implementation. The finding is E1.
- The other half, that the patch violates the criterion, is argued against concrete code. The author adds a test
  that fails on the patch (which resolves the finding by re-run) or disputes it through the normal path.

One new evidence type, one kind. Research support is thin (one summary-only paper), so the bench in section 10
decides whether it stays.

### 6.5 Held-out tests

The strongest anti-gaming measure in research is tests the implementer never sees. The cost is a red check with
no visible test. Recommendation: not now. Rotating seeds, V6 and mutants cover the known cheats. Revisit on
evidence. Decision T5.

---

## 7. UI testing: use what exists, keep it in the sandbox

### 7.1 Two jobs, often confused

1. **A spec-first UI acceptance test.** Written before the feature exists. The page and the button are not there
   yet.
2. **Looking at a built UI** to learn it or to try to break it.

Both need a real browser. Both must keep the pull request's app, and the browser that renders it, inside the
sandbox with no network.

### 7.2 Which existing tool

We are a layer above the browser tooling, so we should not build any. The question is only which tool fits two
hard requirements: **the gate test must re-run with no model call** (a test that asks a model at run time gives
a different answer on a different day, and our sandbox has no network to reach a model anyway), and **an
outside agent must be able to drive it** (our agents are the intelligence; a tool that brings its own agent
loop would be an agent inside an agent, with a second model credential inside the sandbox).

| Tool | Licence | Needs a model call to run a saved test | Saved test | Driven by an outside agent over MCP | Fits |
|---|---|---|---|---|---|
| **Playwright Test + Playwright MCP** (Microsoft) | Apache-2.0 | **No** | plain test code; JUnit with `--reporter=junit` | Yes: `@playwright/mcp`, attaches to a running browser with `--cdp-endpoint`, can emit test code (`--codegen`) | **Yes, web** |
| **Maestro** | Apache-2.0 | **No** | YAML flows | Yes: `maestro mcp` | **Yes, mobile** (its JUnit flag is not verified) |
| TestZeus Hercules | AGPL-3.0 | Yes | Gherkin that a model executes | Yes | No: a model call every run |
| Alumnium | MIT | Appears so | not stated | Yes: `alumnium mcp` | No, unless a replay mode is shown |
| Skyvern | AGPL-3.0 | Yes | none | Yes | No |
| Browser-Use | MIT | Yes | none | Yes | No |
| Stagehand | MIT | Yes; replay without a model shown only on its hosted service | code plus a cache | hosted, needs an account | No |
| Midscene.js | MIT | Yes | YAML plus a cache | no | No |
| Finalrun | Apache-2.0 | Yes | YAML that a model executes | no | No |
| OpenCode's built-in browser (2.x) | n/a | n/a | none | it is the agent's own tool | No: undocumented, reported to need an attached desktop browser, and it would render the PR's pages on the agent's machine |

All rows are research (F) from each project's README on Nov 14, except where marked. The agent-first tools are
good at exploring. Our agents already explore: they need hands and eyes on a browser, and a way to leave behind
a test that runs by itself. Playwright gives exactly those two pieces, and they are the most widely used ones.

So: **Playwright Test is the gate artifact for web, Playwright MCP is how an agent looks and clicks, Maestro is
the same pair for mobile when a repository needs it.** Each is a row in the profile (`tests.kinds.ui`), so a
team that prefers Maestro flows for its web UI changes one line.

Playwright also ships ready-made agent definitions for OpenCode (`npx playwright init-agents --loop=opencode`
writes a planner, a generator and a healer; research (F), playwright.dev). We use the planner and the generator
as OpenCode subagents of the test author, unchanged. We do not install the healer: it may rewrite or skip a
failing test, and our gate tests are pinned. Which files that command writes is **not verified** (spike ST3).

### 7.3 Where things run

```
 agent's OpenCode ── Playwright MCP (unchanged, on the agent host) ── CDP ──┐
                                                                            v
                         sandbox job "preview":  [ the app | headless Chromium ]   no network, read-only root
```

- **`start_preview(at: base|head)`** is the one Heimdall tool. The runner starts a job of image `browser`: the
  app (`serve`, `ready` from the profile) and headless Chromium with its debugging port, in one container with
  no network. The runner makes that debugging endpoint reachable on a loopback port reserved for the calling
  agent. The job ends when the task submits or fails, or after an idle period.
- **Playwright MCP** is a local MCP server in every agent's `opencode.json`, installed at a pinned version on
  the host, started with `--cdp-endpoint http://127.0.0.1:<that agent's port>`. The agent uses its tools as they
  are: `browser_navigate`, `browser_snapshot` (the accessibility tree as text), `browser_click`, `browser_type`,
  `browser_fill_form`, `browser_wait_for`, `browser_console_messages`, `browser_network_requests`.
- The page's code runs only inside the container. The browser can reach only the app beside it. The agent host
  runs Playwright MCP, which is trusted code that sends commands. That is how "sandbox only, no outside calls"
  holds without us writing a browser proxy.
- OpenCode permissions allow the Playwright tools an agent needs and deny the ones that touch the agent's own
  machine (`browser_file_upload`) or are not needed (`browser_run_code_unsafe`, PDF and storage tools). The list
  is in the one template.
- A preview is a running process the runner owns, like any job. `preview.started` and `preview.ended` are facts
  for the record. One preview at a time on the current VM; it is the lowest priority in the queue.

What we give up: the browser steps are not in Heimdall's own record, because they do not pass through our
server. People see the agent's progress lines in the change's thread, and the test file the agent
leaves is the thing that counts.

### 7.4 Job 1: the accessibility contract

A UI test must find things that do not exist yet. Guessed CSS selectors would be red forever. The answer is the
UI twin of the HTTP seam:

- The plan's `ui_handles` lists what the new UI must expose, as a user perceives it:

  ```json
  "ui_handles": [
    {"id": "H1", "role": "button", "name": "Refund"},
    {"id": "H2", "role": "spinbutton", "name": "Amount"},
    {"id": "H3", "role": "alert", "name_contains": "exceeds the payment"}
  ]
  ```
- UI tests locate elements only by role and accessible name or label: a handle from the plan, or something the
  author saw in a snapshot of the base app. The `locator-by-handle` lint refuses raw CSS and XPath.
- The handover lists the handles: "your UI must have a button named Refund".
- A handle is behaviour, not implementation: what a screen reader announces is what the user gets.

The author starts a preview **on base** to learn the existing app: how to log in, how to reach the page the
feature will live on, what the surrounding controls are called. Then it writes Playwright test files, staged
with `put_file`, which are red on base because the handles are not there. The `ui-testing` skill teaches this
flow in one page.

Trade-off: the developer's naming is constrained by a test written first. With a good reason to differ,
`heimdall challenge`, the author amends the handle, an approver re-approves the changed tests.

### 7.5 Job 2: a seat with the `ui` lens, at the head

It starts a preview on the PR head and tries to break a criterion. What it files is a Playwright test file as an
E1 repro. The harness runs it with the `ui` kind. If it fails on an assertion at head, the finding stands. The
exploration is never evidence.

### 7.6 What is deliberately absent

- **No self-healing of gate tests.** A broken locator is a red check and a conversation.
- **No screenshot-diff gate.** Baselines need an approved image per environment and a person to accept each
  change. That is its own workflow.
- **Accessibility scanning is a check:** `axe-core` over the pages a UI test visits, SARIF out, lens `ui`.

### 7.7 Chromium in our sandbox

Known constraints: rootless Podman, `--network=none`, read-only root, `--cap-drop ALL`, default seccomp, a
non-root user, 2 GB, 512 MB of work space, a 2 vCPU host.

| Question | What the docs say | Our position |
|---|---|---|
| Chromium's own sandbox | It needs user namespaces. Playwright's guide: a non-root user with their published seccomp profile; running as root turns the sandbox off | Keep it on, as a second wall around a PR's JavaScript: the `browser` image's allowlist entry carries that seccomp profile, for `browser` jobs only. **Not verified** inside rootless Podman with all capabilities dropped |
| If it cannot work | `--no-sandbox` makes the container the only wall | A security decision for the owner on ST1's evidence (T6). Not pre-built as an alternative |
| Reaching the debugging port of a container with no network | Not documented for our setup | A socket shared with the runner and forwarded to the agent's loopback port. **Not verified**; ST1 finds the working mechanism |
| Shared memory and RAM | The guide recommends `--ipc=host`, which we will not grant | `--disable-dev-shm-usage` or a larger `/dev/shm` for this image; possibly 3 GB for `browser` jobs. ST1 |
| Offline | Playwright MCP attaching over CDP should need no browser download | **Not verified**; ST1 runs with the network off |

If ST1 shows a browser cannot run within limits the owner accepts, the `ui` kind is not declared and plans with
UI behaviours are refused with that reason. The sandbox is not loosened quietly.

---

## 8. The runner is a queue

Three-run acceptance, mutants, counter-implementations, checks and a preview multiply load on two
cores.

1. **Priority, as data on the job kind:** gate-critical (acceptance, evidence verification) before on-demand
   (`run_check`, counter-implementation runs) before background (checks, mutants) before a preview.
2. **Wait, never refuse.** A caller waits its turn inside its deadline. A busy runner never changes an evidence
   tier.
3. **A new head cancels** queued and running background jobs of the old head.

Where the queue lives is the implementer's choice from the code. The three rules are the contract.

---

## 9. The skill, the kinds and the tools

One page of method per kind, with no language in it. Language facts come from the brief and from stack skills
the agent loads on demand. The Playwright MCP tools are OpenCode's, not Heimdall's, and are allowed in the one
template. The author may ask a colleague in the ticket's working room (`@heimdall what did the ticket say about currency?`) and may
hand UI exploration to a subagent.

| Kind | Output | Tools |
|---|---|---|
| `test_plan` | `TestPlan@1` | `get_task`, `ack`, `read_untrusted`, `read_code` (base), `ask_knowledge`, `ask_humans`, `start_preview` (if the profile declares `ui`), `submit_test_plan`, `report_progress` |
| `tests` | `TestSubmission@2` | `get_task`, `ack`, `read_code` (base), `put_file`, `start_preview`, `submit_tests`, `report_progress` |
| `survivors` | `SurvivorAnswers@1` | `get_task`, `ack`, `read_code` (base), `put_file`, `submit_survivors` |

The **test brief** in `get_task`, built by the server, each section failing alone:

- the profile's kinds and how a test of each is laid out, with one existing test file near the area as the
  pattern;
- the names of the stack skills and the `ui-testing` skill that fit this ticket;
- regression seeds, flaky patterns and the heat map for the touched components, when the catalogue has them
  (`KNOWLEDGE_TOPOLOGY.md` section 4);
- after a refusal, the reasons with file and line.

The skill keeps five lines of etiquette that cannot be linted: one behaviour per test; the name says the
behaviour; the failure message says got and want; build inputs in the test, never with the product's helpers;
no shared state.

---

## 10. How we know it works

The test bench of TECH_SPEC 6.7, made concrete. A case is a ticket, a reference implementation and several
planted-wrong implementations, all written before any run.

| Metric | Meaning |
|---|---|
| Wrong-implementation kill rate | Share of planted-wrong implementations the suite fails |
| Bug-validating rate | Share of suites that pass on a wrong implementation and fail on the reference. Target 0 |
| Mutants killed on changed lines | With equivalents and unspecified listed separately |
| Oracle mix | Behaviours per oracle |
| Flake rate | Tests whose result changes in 20 head runs |
| Rounds to accept | Submissions before V0 to V3 pass |
| Survival | Tests unchanged 30 days after merge |
| Cost | Tokens and sandbox seconds per ticket |

Arms, one change at a time: today's flow; plus the plan; plus the survivor loop; plus the critic; and for UI
cases, with and without a preview on base. A mechanism that does not move kill rate or bug-validating
rate is removed. Cases cover at least two languages, because "language-agnostic" is a claim that needs a
measurement. Gold answers before any run, a held-out set never used for tuning, stand-in and production models
reported apart, cost stated first (testing cap $3).

---

## 11. Deletions (C2)

| Deleted | Replaced by | Package |
|---|---|---|
| The eight `"tests/heimdall"` literals (`engine/steps_commit.go`, `effects/acceptance.go`, `effects/test_quality.go` x3, `agenttools/tasks.go`, `mcp/tools.go`, `workflow/definition.go`) | the profile's `paths.tests`, read once | TA1 |
| `placeholderReasons` and the `go/parser` import in `effects/test_quality.go`; `domain/verify/seam.go`; `TestQualityDeps.ForbiddenImports` | rules in the `tests` lens pack, run by the `test-lints` check | TA3 |
| `SandboxRedFirst` reused for acceptance, `SandboxMutation`, the `mutation` workflow row's `With` payload | test kinds and the `mutants` check | TA1, TA4 |
| `TestSubmission@1` (files inline) | `TestSubmission@2` (staged paths, behaviour map) | TA2 |
| The author's `acceptance_criteria` map as input | derived by the server from the plan | TA2 |
| Go text in `skills/test-quality/SKILL.md` (package naming, `httptest`, `app.New`); the skill is renamed to match its role | one neutral page; Go notes move to the Go stack pack | TA2 |
| `tests.mutation_blocks` in policy | "every survivor answered" in the merge rule | TA4 |
| The single-run acceptance | `tests.runs` | TA1 |

---

## 12. Work packages

Format as in `AGENT_INTERACTION_ARCH.md` section 13. Foundations H1 to H3 and `REVIEW_ARCH.md` RV1 (checks) and
RV2 (the `checks` image) come first.

### Spikes

| Spike | Do | Record |
|---|---|---|
| ST1 | On the VM's runner: start the pinned Playwright image as our non-root user with our flags and Playwright's seccomp profile; serve a static page inside the container; start headless Chromium with its debugging port; find a way to reach that port from the agent's user on the host while the container has no network; attach a pinned Playwright MCP to it with `--cdp-endpoint` with the host's network off; run one Playwright test in the container. Try the same without the profile. Measure memory | The working configuration, flags and limits. This decides T6 and whether the `ui` kind exists |
| ST2 | For Go and Python fixtures: run a diff-scoped mutation tool on a small change, time it, and write the 20-line converter to SARIF | Tool, flags, run time per language |
| ST3 | On a scratch machine: run `npx playwright init-agents --loop=opencode` and read the diff; check the planner and generator work as subagents on OpenCode 1.19.5 with Playwright MCP attached over CDP. Separately, on OpenCode 2.x, open the built-in browser with no desktop browser attached | Which files Playwright writes and whether they suit 1.19.5; confirms or corrects the last row of 7.2 |
| ST4 | Run Schemathesis against `pay-demo` served on loopback, offline; read its licence | Whether the `contract` kind ships in the template |

### TA1. Test kinds, the tests folder, runs, the queue

- **Goal:** `tests.kinds`, `tests.runs`, `paths.tests` as the only source; one executor for kinds; the queue
  rules of section 8.
- **Read first:** TECH_SPEC 12.1, 12.2, 12.2b; `ports/types_profile.go`; `adapters/profileyaml`;
  `effects/test_quality.go`; `effects/acceptance.go`; `adapters/rootlessrunner`.
- **Change:** profile fields; replace the eight literals; acceptance and red-first both call the kind executor
  `tests.runs` times and use the existing `verify.Stability`; job priority on the runner API.
- **Tests first:** profile validation (a kind's `run` needs `{dir}`; `ready` must be loopback); the stability
  rule on N runs; queue ordering and cancel-on-new-head with a fake runner.
- **Done when:** `grep -rn '"tests/heimdall' internal cmd` is empty outside tests; on the VM three reviewers and
  an acceptance run overlap with no refusal; a test made flaky on purpose is quarantined on head.
- **Do not:** leave a default value for the tests folder in code.

### TA2. The plan and staged submission

- **Goal:** kind `test_plan`, `TestPlan@1`, V0, `TestSubmission@2` with `put_file`, the plan in the status and
  PR comment, sign-off on the plan in sensitive domains.
- **Read first:** `domain/testset`, `mcp/tools.go` `submitTests`, `engine/steps_agent.go`, `cmd/heimdall-server/signoff.go`.
- **Tests first:** plan validation (every rule in 4.2, each with its field path); behaviour-to-test mapping both
  ways; fuzz decode of the plan as `testset` does today.
- **Done when:** a live ticket shows the plan before any test code; a plan with no negative case is refused
  naming the criterion.

### TA3. Test lints as rules

- **Goal:** the `tests` lens pack's rules for the fixture languages; the `test-lints` check as V1; delete the Go
  checks.
- **Tests first:** each rule has a file it fires on and one it does not, per language (rule tests, run by the
  pattern engine in CI).
- **Done when:** a Python fixture ticket and a Go fixture ticket are both refused for a skipped test, by the
  same server code.

### TA4. Mutants and the survivor loop (after ST2)

- **Goal:** the `mutants` check, kind `survivors`, the three answers, "every survivor answered" in the merge
  rule.
- **Read first:** `domain/gate`, `workflow/definition.go` (the `mutation` row), `REVIEW_ARCH.md` 4.1 to 4.4.
- **Tests first:** the answered-survivors predicate (each answer kind; a new head resets what must be answered);
  the kill check (fails on the mutant, passes on head); the gate table row.
- **Done when:** on a fixture a deliberately weak suite produces survivors; the author kills one, marks one
  unspecified, and the question reaches the tests channel.

### TA5. Gaming signals and the seed

- **Tests first:** each signal on a synthetic diff; seed derivation from ticket and SHA.
- **Done when:** a planted special-case implementation is caught by a signal or by the next head's seed.

### TA6. Services and a served app

- **Goal:** `services`, `serve`, `ready` in the profile and the runner, each with an escape test on the VM, as
  the runner's `doc.go` requires for anything that touches isolation.
- **Done when:** a fixture with a database runs its acceptance tests with no egress.

### TA7. The contract kind (after ST4)

- **Done when:** a response that breaks the OpenAPI file at head is an E2 finding with no model call.

### TA8. The critic

- **Goal:** kind `critique`, evidence `counter_impl`, the patch run.
- **Tests first:** patch application rules (applies to base only, bounded, cannot touch `paths.tests`); the
  finding's verification (all tests pass means E1).
- **Done when:** the bench's critic arm raises wrong-implementation kill rate. If it does not, the package is
  reverted, not kept.

### TA9. UI tests with Playwright (after ST1, ST3 and TA6)

- **Goal:** the `browser` image; the `preview` job in the runner and `start_preview`; Playwright MCP in the one
  agent template, pinned and installed on the host, with its allowed tools listed; the `ui` kind; `ui_handles`
  in the plan; the `locator-by-handle` rule; the `ui-testing` skill; Playwright's planner and generator as
  subagents; the `axe` check.
- **Read first:** `adapters/rootlessrunner/api.go` and `policy.go`; `deploy/agents`; section 7; ST1's and ST3's
  results.
- **Tests first:** preview ownership (only the task that started it; one at a time; ended on submit, failure or
  idle); the template's permission list (every Playwright tool is either allowed or denied by name, none left
  to a default); the handle rule.
- **Done when:** on a fixture with a web page, a UI ticket gets a red Playwright test before the feature and a
  green one after, the handover lists the handles, and the agent's progress shows in the change's thread.
- **Do not:** write a browser proxy or wrap Playwright's tools. Do not let a page from a pull request render
  anywhere but the sandbox. Do not install the healer.

### TA10. Test knowledge

- Regression seeds, flaky memory and the heat map in the test brief (`KNOWLEDGE_TOPOLOGY.md` P3).
- **Done when:** a seed from a real fix commit yields a test that fails on the parent and passes on the fix.

**Order.** ST1 to ST4, TA1, TA2, TA3, TA4, TA5 (the core, no new image beyond `checks`), TA6, TA7, TA8, TA9,
TA10.

---

## 13. Decisions for the owner

| # | Decision | Recommendation | Why |
|---|---|---|---|
| T1 | The test plan step | Yes | The cheapest human checkpoint; every later check hangs off it |
| T2 | Sensitive-domain sign-off on the plan | Yes | A person can read a plan |
| T3 | Mutation blocks by "every survivor answered", no score | Yes | A number invites gaming; a list can be audited |
| T4 | The critic | Keep only if the bench shows gain | Thin research support |
| T5 | Held-out tests | No | Developer experience; revisit on evidence |
| T6 | If Chromium's sandbox cannot run in our container: allow `--no-sandbox` with the container as the only wall, or drop the `ui` kind | Decide on ST1's evidence | A security trade-off that is the owner's |
| T7 | Use Playwright as it is (its test runner for the gate, its MCP server for the agent, its planner and generator as subagents), with the browser inside the sandbox. Maestro the same way for mobile. No agent-first tool that needs a model call per run, and no browser tooling of our own | Yes | Deterministic re-runs, sandbox-only, and nothing to build but one `start_preview` tool |
| T8 | Memory limit for `browser` jobs | Decide after ST1 | 16 GB host, 2 vCPU |
| T9 | Screenshot-diff testing | Not in this plan | Needs a baseline-approval workflow |

---

## 14. Non-goals

- No test generation from the implementation.
- No self-healing or auto-skipping of gate tests.
- No model deciding flakiness, pass or fail.
- No screenshot-diff gate, no cross-browser matrix, no load or performance testing.
- No unit tests for the developer's internals. Heimdall tests behaviour through the seam and the UI.
- No coverage target.
- No page from a pull request rendered outside the sandbox. No browser tooling of our own.

---

## 15. Not verified

- Everything in 7.7, and the image's size.
- Which files Playwright's `init-agents` writes for OpenCode, and OpenCode's built-in browser beyond its permission rule (ST3).
- Maestro's JUnit output and its web mode offline.
- The mutation tool and converter per language, and their run time (ST2).
- Schemathesis offline, and its licence (ST4).
- That the production model writes a valid plan and working Playwright tests from snapshots. The bench decides.
- That the critic helps.
- Research marked (S) in section 2.
- `make fast` and `make verify` were not run for this document: it changes no code.

---

## 16. Sources

Research helpers, Nov 14, 2026. (F) fetched, (S) summary only.

- Konstantinou, Tambon, Papadakis, "On the risk of coding before testing", arXiv 2607.05139 (F)
- Mathews, Nagappan, "Design choices made by LLM-based test generators prevent them from finding bugs", arXiv 2412.14137 (F)
- Meta, "Mutation-Guided LLM-based Test Generation at Meta", arXiv 2501.12862 (F)
- Google, "Practical Mutation Testing at Scale", arXiv 2102.11378 (S)
- METR, "Recent Frontier Models Are Reward Hacking" (Jun 2025) (F); ImpossibleBench, arXiv 2510.20270 (abstract)
- Anthropic, "Finding bugs with agentic property-based testing" (Jan 2026) (F)
- "On the Diffusion of Test Smells in LLM-Generated Unit Tests", arXiv 2410.10628 (F)
- Playwright: test agents, test reporters, Docker guide (F); microsoft/playwright-mcp README (F)
- READMEs of TestZeus Hercules, Maestro, Alumnium, Skyvern, Browser-Use, Stagehand, Midscene.js, Finalrun (F)
- OpenCode 2.x permissions and tools pages (F); thinkite/thinkite issue 31 (third-party, F)
- cargo-mutants `--in-diff`; go-mutesting; Infection; Stryker report schema (S)
- Schemathesis CLI docs (S); its evaluation, arXiv 2112.10328 (S)
- Go wiki TestComments; Go 1.25 release notes (F)
