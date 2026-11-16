# Review architecture: one pipeline, any language, reviews people act on

Status: architecture and plan, third pass (Nov 14, 2026). Nothing here is implemented.

Who builds each package, in what order, and how each piece is proven: `docs/BUILD_PLAN.md`.

Read `docs/AGENT_INTERACTION_ARCH.md` first. Its construction rules C1 to C9 and foundations F1 to F6 (facts and
views, the kind table, seats, checks as data, rooms and `@`, the generic agent host) are assumed here.

Builds on, and does not replace:

- `docs/REVIEW_MOAT.md`: what is built (cards, code graph, brief, learning loop) and what was measured.
- `docs/CONTEXT_CAPTURE_PLAN.md`: how a developer's knowledge is captured (gaps G1 to G26).
- `docs/KNOWLEDGE_TOPOLOGY.md`: the four scopes (codebase, team, role, person) and the stores (gaps K1 to K16).

Those say what knowledge to have. This file says how a review is run so the knowledge becomes findings a person
fixes, in any language, with one mechanism per job.

Labels: **verified in code** = read in this repository on Nov 14. **research (F)** = a helper fetched the primary
page. **research (S)** = search summary only. **mine** = my own knowledge, not re-checked today. **not
verified** = nobody checked; each is behind a named spike.

---

## 0. The one-sentence logic

**Tools find what tools can find, in any language, and report it in one format. Models look only where tools
cannot, guided by cards. Every unproven claim meets one attempt to refute it before a person sees it. Who looks
at what is decided by seats and lenses in policy, never by a prompt.**

The whole review side uses five mechanisms and no others:

| Mechanism | Is | Carries |
|---|---|---|
| **Check** | A tool run declared as data, SARIF out | Everything deterministic: patterns, secrets, dependencies, infrastructure, migrations, API breaks, co-change, mutants |
| **Card** | A short scoped note with one checkable question | What must be looked at on this change: a person's rules, a lens checklist, team conventions |
| **Skill** | A page of method the agent loads on demand (OpenCode) | How to do a kind of work: the review method, writing a repro test, a language's pitfalls |
| **Seat** | A reviewer agent with lenses, optionally a person | Who answers which cards and which tool results |
| **Evidence tier** | E1 to E4, verified by code | What may block |

---

## 1. Where we are (verified in code and `PROGRESS.md`)

Works live: three reviewer agents with personal cards (KAN-23: all three found both planted bugs, two findings
came only from cards); the brief; evidence tiers; the PR comment; the gate refresher.

| # | Weakness | Evidence |
|---|---|---|
| W1 | All reviewers run the same skill and differ only by cards. Nobody is asked to look at security, infrastructure, migrations or the contract as such | `skills/reviewer/SKILL.md` |
| W2 | Deterministic evidence is almost empty: `run_check` has one kind, `go_vet`, as a Go constant. The skill names `staticcheck`, `oasdiff`, `semgrep`, which do not exist here | `mcp/tools.go` `runCheckSchema`, `ports.CheckGoVet` |
| W3 | The deterministic layer is Go-only by construction: a check is an enum value in server code. TECH_SPEC 12.2b already specifies per-repo `evidence:` tools in the profile. That was never built | `ports/types_work.go`, `ports/types_profile.go` |
| W4 | The code graph parses Go with `go/parser` and every other language with line regexes: two paths, unequal quality | `app/codegraph/tag.go`, `langs.go` |
| W5 | Nothing stands between an unproven finding and a person: a high E4 finding opens a human card | TECH_SPEC 11.1 |
| W6 | No notion of scope: a true remark about untouched code is treated like a finding on the change | `finding.Input` |
| W7 | A reviewer may leave a card unanswered (the server only notes it) and may contradict itself: reviewer-1 marked `refund-cap.md` "holds" while filing against it | skill text; `PROGRESS.md` KAN-23 |
| W8 | The capture loop has never run for a real person on the VM, and the one bench where a card helped used cards written with the answer in them | `CONTEXT_CAPTURE_PLAN.md` G1, G5, G6 |
| W9 | The reviewer role holds 18 tools, six of them for dead or human-only features | `mcp/tools.go` |
| W10 | The reviewer skill's examples and repro rules are Go (`package pay142_test`) | skill text |
| W11 | No comment budget: a review may carry 50 findings | `Review@1` |
| W12 | Our numbers are one synthetic PR, scored by its author, on a stand-in model | `REVIEW_MOAT.md` section 4 |

---

## 2. What the research says

| Finding | Number | Trust |
|---|---|---|
| Developers rejected most AI review comments. Of rejections, false positives were 43.3% and "valid but not how we do it" 42% | 31,073 comments, 239 repos, 56.3% rejected | research (F), arXiv Jul 2026 |
| Every serious product has a verify or gatekeeper pass before posting | Bugbot, Ellipsis, Claude Code Review | research (F) for the designs |
| Adversarial kill agents remove most false findings | about 79 to 83% | research (F), self-reported, arXiv 2604.19049 |
| Aggregating independent reviews raises F1 | up to +43.67% | research (F), SWR-Bench |
| Find-everything agents are noisy; cautious prompts miss bugs; better tools helped more than prompts | qualitative | research (F), CR-Bench; Cursor |
| LLMs alone are poor vulnerability finders and good triagers of tool output | IRIS about 85% false discovery; LLM filter removes 34 to 38% of tool false positives, no real issue lost | research (F) |
| SARIF is emitted natively by Semgrep CE, gitleaks, Grype, Checkov, hadolint, gosec | flags fetched | research (F) |
| SARIF from Trivy, kube-linter, zizmor, actionlint, golangci-lint, bandit | flags not confirmed today | research (S) or mine |
| `oasdiff` and `buf breaking` emit JSON and JUnit, not SARIF | need a converter | research (S) |
| Semgrep's engine is LGPL-2.1. Semgrep-maintained rules are licensed for "internal business use" only, and vendors may not use them in competing products | fetched from docs.semgrep.dev/licensing | research (F) |
| Opengrep (the fork) has an LGPL-2.1 engine. The licence of its rules repository is unknown | | research (F) engine; rules **not verified** |
| Trivy is Apache-2.0 and runs offline with a pre-fetched database. Its GitHub Action was compromised in March 2026 through moved tags | offline flags fetched; incident from summaries | research (F) / (S) |
| Mining co-change rules from version history predicts related changes: the top 3 suggestions contained a correct location in roughly 64 to 70% of cases | Zimmermann et al., ROSE, 2004/2005; the two figures conflict | research (S) |
| Evidence that team or personal context improves review | **none found in public** | ours to prove |

What follows from it:

1. Our tiers already make E1 to E3 findings true. The false-positive problem lives in E4. The refute step goes
   there.
2. "Valid but misaligned" is what cards are for. Nobody has shown publicly that context helps. If our ablation
   shows it, that result is the product.
3. Keep several independent reviewers: aggregation is the one multi-agent pattern with a measured gain.
4. A language-agnostic deterministic layer is feasible because the tools already agree on SARIF. The server
   should know SARIF, not tools.
5. We may not ship Semgrep's rules. We ship our own, small and owned, inside the lens packs.

---

## 3. The pipeline

One pipeline. Each stage is a row in the workflow table (`domain/workflow/definition.go`), which is where
`Missing` comes from (`AGENT_INTERACTION_ARCH.md` F1). A stage that has nothing to do for a change is done at
once. No stage has an alternative. The server opens the work each stage needs. How a seat does it, and whom it
asks in the ticket's working room, is the seat's business.

```
 PR head
  S0 check     run the profile's checks on base and head; keep what is new          no model
  S1 route     lenses this change needs -> seats that hold them                     no model
  S2 find      each seat: base pass + its cards + its leads, independent            model
  S3 verify    E1 run, E2 re-run, E3 re-query                                       no model (exists)
  S4 refute    one attempt per unproven finding, by another seat, citing code       model, checked by code
  S5 present   scope, budget, order                                                 no model
  S6 learn     outcomes weight cards, rules and seats; reasons become proposals     no model (exists, extended)
```

---

## 4. S0: checks

### 4.1 A check is a row in the repo profile

TECH_SPEC 12.2b already gives the profile an `evidence:` list. It is renamed `checks:` and built.

```yaml
# .heimdall/profile.yaml  (proposed by Heimdall's setup PR, merged by a person, read at the merge base)
checks:
  - id: patterns
    lens: security
    when: ["**/*"]                      # globs over the files the change touches
    image: checks                       # a name on the server's image allowlist; the digest lives there
    run: "opengrep scan --config /rules --config .heimdall/rules --sarif-output /out/result.sarif /work"
    baseline: true                      # also run on base; keep only results that are new
  - id: migrations
    lens: data
    when: ["**/migrations/**", "**/*.sql"]
    image: checks
    run: "squawk-sarif /work > /out/result.sarif"
    baseline: true
  - id: api-break
    lens: contract
    when: ["**/openapi*.y*ml"]
    image: checks
    run: "oasdiff-sarif /base /work > /out/result.sarif"
    baseline: false                     # the tool compares base and head itself
```

The contract of a check, the whole of it:

- It runs in the sandbox with the job isolation that exists (no network, read-only root, no secrets).
- The head tree is at `/work`, the base tree at `/base`, both read-only. It writes SARIF 2.1.0 to
  `/out/result.sarif`, bounded in size.
- `image` must be on the allowlist. `run` comes only from the profile a person merged and the runner promoted
  (the runner already consults a promoted profile set, `rootlessrunner/runner.go`). An agent never supplies a
  command.

The server has one executor for checks and one SARIF reader. It contains no tool name. The repo's own linters
(`ruff`, `eslint`, `golangci-lint`, `clippy`) are simply more rows the repo adds.

### 4.2 The `checks` image and the starting catalogue

Heimdall builds and publishes one image, `checks`, pinned by digest: the tools below at fixed versions, their
offline databases and rule sets fetched by the warm job (the only job with egress), and a few 20-line converters
for tools that do not speak SARIF. A converter is tested with golden files and lives in `deploy/checks/`. It is
part of the image, not of the server.

| Lens | Check | Tool | Licence | SARIF |
|---|---|---|---|---|
| security | patterns | Opengrep or Semgrep CE engine, **with rules Heimdall writes and owns** | engine LGPL-2.1, run as a separate process | native (F for Semgrep) |
| security | secrets | gitleaks | MIT | native (F) |
| security, infra | dependencies, Dockerfile, Kubernetes, Terraform, Helm | Trivy (`vuln`, `misconfig`) | Apache-2.0 | native (mine); offline flags (F) |
| infra | Dockerfile detail | hadolint | GPL-3.0, separate process | native (F) |
| infra | CI workflows | zizmor, actionlint | MIT | **not verified** |
| infra | Kubernetes detail | kube-linter | **not verified** | native (S) |
| data | SQL migrations | squawk | **not verified** | converter |
| contract | OpenAPI breaking changes | oasdiff | **not verified** | converter from its JSON |
| contract | protobuf breaking changes | buf | **not verified** | converter from its JSON |
| contract | forgotten companions | `heimdall-mine` (4.6) | ours | native |
| tests | surviving mutants | per language (`TEST_AGENT_ARCH.md` 6.2) | per tool | converter |

Every "not verified" is closed by spike SR1 before the tool enters the image. A tool whose licence does not
allow shipping it inside a product image is left out, and the catalogue says so. Images and tools are pinned by
digest and commit, never by tag: the Trivy incident was moved tags.

**Rules we own.** Semgrep's maintained rules may not be shipped in a product. So each lens pack carries its own
small pattern rules (section 5.2), written for the cards they serve, reviewed like code, and the repository
adds its own under `.heimdall/rules/`. A team that wants a larger third-party rule set for internal use adds it
to their own profile under their own licence. That is their row, not ours.

### 4.3 New in this change only

For a check with `baseline: true` the server runs it on base and head and keeps a head result only if no base
result has the same fingerprint:

```go
// internal/domain/sarif/new.go
// Fingerprint = rule id + file path + hash of the reported lines' text with whitespace collapsed.
func New(base, head []Result) []Result
```

Line numbers are not part of the fingerprint, so code that merely moved is not "new". Base results are cached by
`(base sha, check id, image digest)`. This one pure function removes the largest source of tool noise: old
problems shown on every PR.

### 4.4 One shape for everything a tool says

```go
// internal/domain/sarif/result.go
type Result struct {
    Check   string    // profile check id
    Lens    string
    Rule    string
    Level   string    // error | warning | note
    File    string
    Lines   [2]int
    Message string    // untrusted text; reaches a model only wrapped
    Finger  string
}
```

### 4.5 Two routes out of S0

| Route | When | What happens |
|---|---|---|
| **Enforced** | The policy at the merge base lists the rule: `checks.enforce: ["secrets/*", "migrations/adding-not-nullable-field"]` | The server files the finding: evidence `tool_result`, tier E2, no model. A re-run on the next push resolves it |
| **Lead** | Every other new result | It goes to the seat that holds the result's lens (S1). The seat must answer it: `confirmed` (it becomes an E2 finding citing the run) or `dropped` with a one-line reason |

The second route is the model as triager, which is the configuration research shows working.

`run_check(check_id)` replaces today's `run_check(kind)`: a seat may re-run any check of the profile during S2.
The enum `go_vet` and `ports.CheckGoVet` are deleted.

### 4.6 Mining history: forgotten companions

A classic, deterministic, language-blind signal: files that have always changed together. If `A` changed and
`B` did not, somebody may have forgotten the migration, the schema, the docs, the other half of a pair.

```
transactions  = the last N commits on the base branch (N from policy, start 2000), no merges,
                commits touching more than 30 files skipped
support(A,B)  = commits that changed both
confidence    = support(A,B) / commits that changed A
rule A => B   when support >= 5 and confidence >= 0.8   (both from policy)
```

For each changed `A` with a rule `A => B` where `B` is not in the change, `heimdall-mine` emits one SARIF result
on `A`: "B changed in 14 of the last 16 commits that changed A. B is not in this change."

- It is association-rule mining restricted to pairs. Larger item sets cost far more and are left out until the
  measured conversion of pair leads says they are worth it.
- It is always a **lead**, never an enforced rule. Its published accuracy is about locating related files, not
  about bugs, and nobody has published its precision as a review warning (research (S)).
- It needs history, which a tarball at one SHA does not have. The warm job keeps a blobless bare mirror of the
  repository in the cache volume (commits and trees only). The check reads it offline.
- `heimdall-mine` is a small Go binary in the `checks` image. It is a check like any other: a row, SARIF out.

The same counting, over our own event log instead of git, gives the bug-class heat map of
`KNOWLEDGE_TOPOLOGY.md` 4.3: which lens found what, in which component, how often. That is section 9.

### 4.7 The symbol index, one path for all languages

The code graph needs definitions and uses. Today Go gets `go/parser` and everything else gets line regexes.

One path: a `symbols` job in the sandbox runs universal-ctags over the tree and writes one JSON line per
definition (name, kind, file, line, scope). Uses are found as today, by token match, and are still labelled
"name match only". `codegraph` keeps its graph, ranking and "removed but still used" logic and loses its two
taggers.

**Not verified** (spike SR2): universal-ctags JSON output in the image, its licence terms for inclusion, its run
time on the fixtures, and that the bench cases the graph solved (G1, G2 in `REVIEW_MOAT.md`) are still found.
If the index loses those cases, the plan changes here, in this section, before any code is deleted.

---

## 5. S1 and S2: seats, lenses and cards

### 5.1 Everything a model is told to look at is a card

A card has a scope, where it applies, one question, and how to prove a violation. `rulecard.Card` already has
`AppliesTo`, `Symbols`, `Triggers`, `Check`, `Proof`, `Kind`, `Status`. It gains `Scope` and `Lens`.

| Scope | Written by | Lives in | Example |
|---|---|---|---|
| `person` | a developer, through their own agent | the cards volume, private | "The bank drops batch lines above 500" |
| `lens` | Heimdall's template, then the repo | `.heimdall/lenses/<lens>/*.md` | "Every new handler decides who may call it before touching data" |
| `team`, `codebase` | the team, by merged PR | `.heimdall/knowledge/` (`CONTEXT_CAPTURE_PLAN.md`) | "Refunds use the ledger clock" |

One ranker, one budget, one answer format (`rule_checks`), one learning loop for all of them.

**Cards say what must be checked on this change. Skills say how to do a kind of work.** Knowledge about a
language or a stack is not a card and not a prompt: it is an OpenCode skill (`stack-python`, `stack-terraform`,
`stack-sql`), served by the Heimdall server and loaded by the agent on demand (`AGENT_INTERACTION_ARCH.md` F6).
The brief names the skills that match the files in the change, and the agent loads the ones it needs. So a
Python change gets Python pitfalls and a Terraform change gets Terraform ones, with no per-language code or
prompt in the server. The review method itself is one language-neutral skill.

### 5.2 A lens pack

A lens is a folder. Heimdall ships templates in `deploy/lenses/`. The setup PR copies the ones a repository
needs into `.heimdall/lenses/`. From then on the repository owns them.

```
.heimdall/lenses/security/
  lens.yaml            # id: security;  when: ["**/*"]            (globs that make the lens needed)
  authz-new-route.md   # cards, each with applies_to, triggers, check, proof
  input-to-query.md
  secrets-in-logs.md
  rules/               # pattern rules the `patterns` check loads; ours, small, reviewed
    sql-concat.yaml
```

Starting packs: `correctness` (the base pass's aids: callers, removed symbols, error paths), `security`, `data`,
`infra`, `contract`, `operability`, `tests`, and later `ui`. Starting stack skills, in `skills/stacks/`: the
languages and formats the fixtures use, then by demand. A repository adds its own under `.heimdall/skills/`.

A card stays a place to look, never evidence. A lens card can never reach E3. Only a person's confirmed
card can, by the ownership rule that exists.

### 5.3 Seats: who covers what

From `AGENT_INTERACTION_ARCH.md` F3. A team picks the shape that fits. All four are the same mechanism.

```yaml
# A. Day one, no captured context: three template reviewers cover every lens.
seats:
  reviewer-1: {role: dev,      lenses: [correctness, contract, tests]}
  reviewer-2: {role: security, lenses: [security, data]}
  reviewer-3: {role: infra,    lenses: [infra, operability]}

# B. A security engineer lends the security seat her knowledge.
people:
  priya: {github: priyak, chat: U0456, seat: reviewer-2}

# C. A path always needs a lens.
reviewers:
  require_lens: {"deploy/**": [infra], "internal/auth/**": [security]}

# D. More seats for a larger team: add agents, give each a lens set and optionally a person.
```

Policy validation, at load: every lens the repository adopted is held by at least one seat; a seat names a real
agent; a person sits on at most one seat. So "a needed lens has no reviewer" is a configuration error caught
before any review, not a runtime case.

### 5.4 The exact mapping, end to end

Nothing here lives in OpenCode or in a prompt file on a host.

| Step | Where | What |
|---|---|---|
| 1 | `policy.Seats`, `policy.People` at the merge base | seat -> lenses, seat -> person. The seat's `role` is the Switch role it holds, so people and agents reach it as `@security` |
| 2 | S1 `route` step (pure): diff paths against each `lens.yaml` `when`, plus the lens of every new result | the lenses this change needs |
| 3 | S1: seats holding a needed lens, plus the author's picks, plus `require_lens`, up to `reviewers.count` | `reviewers.picked{seat, lenses, person}`, a fact |
| 4 | `reviewbrief` compiler, inside `get_task` | `obligations` = ranked cards of the seat's person and its lenses. `leads` = new results whose lens the seat holds. `skills` = the names of the stack skills that match the changed files. Each names what was left out for budget |
| 5 | The seat's agent | does the base pass, answers each obligation in `rule_checks` and each lead in `lead_checks` |
| 6 | `submit_review` validation | refused unless every obligation and every lead in the brief is answered, and unless answers and findings agree (5.5) |

Every picked seat does the full base pass. The overlap is deliberate: it keeps the consensus rule and the
aggregation gain. Lenses add coverage on top.

Seats work in the ticket's working room and may talk (`AGENT_INTERACTION_ARCH.md` section 7): `@infra does the deploy job
run migrations first?`, `@test-author which fixture has a partial refund?`. Inside its own session a seat may
split the work across OpenCode subagents, one per hypothesis or file group. Two rules keep the review sound: a
seat submits its own review before it reads or discusses another seat's findings, and what a colleague says is
information, never evidence.

### 5.5 Answers must be complete and consistent (fixes W7)

Pure checks on submit:

- Every card in `obligations` has one `rule_checks` entry. Every result in `leads` has one `lead_checks` entry.
- `holds` for a card is refused when a finding in the same review cites that card, and the reverse.
- A `confirmed` lead has a finding that cites its run. A `dropped` lead has a reason.

### 5.6 What was considered

| Choice | Chosen | Why |
|---|---|---|
| A specialist agent and prompt per lens, or lens cards on seats | Cards on seats | One mechanism that already works live; a template seat *is* the specialist, with no new agent type |
| Each reviewer is always a person, or a seat that may have a person | Seat | Coverage on day one without captured context; a person upgrades a seat instead of being a precondition |
| Stack knowledge as cards in the brief, or as skills loaded on demand | Skills | The harness already lists skills and loads only the ones the model asks for; nothing to rank or budget in the server |
| Server-run coordination between seats, or seats that talk | Seats talk; the server only opens what is missing | Switch and OpenCode already carry conversation and delegation |
| Lens text in the skill, or as cards | Cards | Prose is not answered, ranked or learned |
| Reassign an uncovered lens at run time, or refuse the configuration | Refuse at load | One path, no surprise |

---

## 6. S4: one attempt to refute

After every picked seat has submitted or timed out, each finding still unproven (E4) and of medium severity or
above goes to one seat that did not file it, as a `refute` task, with one question: **what in the code stops
this?**

| Answer | Requires | Effect |
|---|---|---|
| `refuted` | A file and line range at head that is not the finding's own lines, and one sentence. The server checks the location exists | The finding is never a human card and never blocks. It is shown collapsed with the citation |
| `not_refuted` | A sentence saying where it looked | The finding goes on. A high one opens a human card, marked "a second reviewer could not refute this" |
| `proved` | E1 to E3 evidence | The seat is added as a reporter. S3 verifies it like any evidence |

A finding is "refuted" because a `finding.refuted` fact exists and no later evidence was filed for it. Nothing
sets a state (C3). E1 to E3 findings skip this stage.

Peers, once, with a code citation: three seats already hold the tools and have just read the change; one attempt
bounds cost; "it looks fine" is refused. The risk is a wrong refutation hiding a true finding. It stays visible,
the filer can add evidence, a refuted finding that later proves true counts against the refuter (S6), and the
bench measures how many true findings this stage kills.

---

## 7. S5: what is shown

### 7.1 Scope (fixes W6)

Computed by code from the diff and the symbol graph, never declared by a model:

| Scope | Rule | Treatment |
|---|---|---|
| `changed` | The location overlaps lines the diff added or changed | Normal |
| `caused` | Unchanged lines that the graph links to a changed symbol | Normal; the comment names the change that causes it |
| `pre_existing` | Neither | Never blocks, never a line comment. Listed under "Noticed, not from this change", offered as a ticket |

### 7.2 Budget and order (fixes W11)

- Line comments: the blocking set first, then by severity and tier, capped by policy (start 10 per round). The
  rest are collapsed in the summary.
- One problem, one comment. Every comment has four lines: what breaks and for whom; the evidence in plain words;
  how to see it yourself; what would clear it.
- No fix patches from reviewers. Agents establish evidence, the author fixes.

### 7.3 The reviewer's tools (fixes W9)

By kind, from the kind table:

| Kind | Tools |
|---|---|
| `review`, `critique` | `get_task`, `ack`, `read_untrusted`, `read_code`, `search_code`, `find_symbol`, `read_card`, `ask_knowledge`, `run_check`, `put_file`, `submit_review`, `report_progress` |
| `refute` | `get_task`, `ack`, `read_code`, `search_code`, `find_symbol`, `run_check`, `put_file`, `submit_refutation` |

The read tools and `propose_command` are available to every agent at any time, so a seat that a person or a
colleague addresses can look things up and answer with no task open.

`get_change` folds into `get_task`. Pack, knowledge-staging and profile-approval tools leave the role (section
11).

### 7.4 Proof in any language (fixes W10)

An E1 repro is a test file in the repository's own test framework. The brief's `how_to_test` section comes from
the profile: the test directory, the command, and one existing test file near the change as the pattern to
follow. The harness runs it with the profile's command and reads JUnit. The skill's Go example is removed.
Stack skills carry language-specific testing notes.

---

## 8. Bringing captured context to life: the activation path

`CONTEXT_CAPTURE_PLAN.md` lists 26 gaps. One thin path through them gives the first non-circular proof that a
real person's knowledge changes a real review. Do it first.

| Step | From | What | Done when |
|---|---|---|---|
| A1 | G1, G9 | Per-person human tokens on the VM | A person runs `heimdall cards push` and `confirm` against the VM with no operator |
| A2 | G2, G3, G11 | `cards whoami`, refuse a person mismatch, `repo:` scoping, refuse a tracked signals file | Each refusal has its test |
| A3 | G5 | One real capture: the owner runs `scan` and `prompt` on their own machine for the fixture repo and pushes the cards they accept. Record time and hashes | Cards on the VM, written by someone who has seen no planted bug |
| A4 | G6 | Someone without the cards' text plants bugs those cards should catch, bugs they should not, and a clean change | Cases dated after the cards |
| A5 | this doc | Template seats, the security and data lens packs, three checks, the refute stage, live | On the VM |
| A6 | G19 | Ablation on the production model, one thing at a time: person cards, lens cards, stack skills, checks, refute, and talk between seats | A table in `REVIEW_MOAT.md`: recall, precision, noise on the clean change, tokens, for every arm, including the ones that did not help |
| A7 | G13, G14, G22 | The authoring path: `heimdall context`, the context-author skill, `.heimdall/knowledge/` | A developer's agent writes a knowledge file, a PR merges it, a seat cites it as E3 |

After A6 the rest of the two knowledge documents is ordered by that table. Their open decisions keep their
recommendations. One mapping: their `disciplines` field is `lenses` here.

---

## 9. S6: what the loop learns

All folds over the event log. `reviewlearn` already weights person cards.

1. **Every card and every check rule gets the same weight** from what happened to the findings and leads it
   produced. A rule whose leads are dropped again and again is muted for that repository and listed for a
   person to delete. This covers co-change rules too.
2. **Refutations have a record** per seat: refutations later overturned. A seat that refutes carelessly loses
   the force of its refutations: they are shown as opinions and hide nothing.
3. **Every dismissal asks why**, with a category: wrong, out of scope, intended trade-off, preference. The last
   two become convention proposals for the path's owner (`KNOWLEDGE_TOPOLOGY.md` 7.14).
4. **The heat map** is a count: per component, findings by lens and outcome. It feeds the ranker (a lens that
   keeps finding real problems in a component ranks its cards higher there) and the test brief.

No model takes part in any of these. Proposals are written by the existing proposer and wait for a person.

---

## 10. Safety

Nothing here widens what an agent can do.

- A seat reads untrusted text and holds no credential and no egress. After the addressing change it cannot be
  woken by a person or wake another agent.
- Tool output, lens cards and another seat's finding all reach a model wrapped as data. Skills are
  instructions, so only Heimdall's own and the repository's default-branch skills are served, never a PR's.
- The profile, its checks, the lens packs and the rules are read at the merge base, so a PR cannot edit the
  checks it is reviewed with. A change to them is itself reviewed under the old ones.
- A check's command comes only from a profile a person merged, runs with no network and no secrets, and cannot
  name an image off the allowlist.
- A refutation can hide only an unproven finding. It never touches an E1 to E3 finding, an approval or the gate.
- The injection suite gains a case per new input: a result message, a lens card body, a refutation, a message
  from another seat.

---

## 11. Deletions (C2)

| Deleted | Replaced by | Package |
|---|---|---|
| `ports.CheckGoVet`, `SandboxStatic`, `SandboxContract`, `SandboxMutation`, `runCheckSchema` enum, `maxChecksPerTask` constant (moves to policy) | the check executor and `run_check(check_id)` | RV1 |
| `domain/verify/e2.go`, `vet.go` tool-specific judging | SARIF reader | RV1 |
| `codegraph/tag.go` Go parser path and `langs.go` regex taggers | the symbol index | RV4, only after SR2 |
| `agenttools/go_source.go` | the profile's `how_to_test` brief section | RV6 |
| Go examples and pack text in `skills/reviewer/SKILL.md`; `skills/reviewer/examples/*.json` that are Go-specific move to the Go stack pack | one neutral page | RV6 |
| `read_pack`, `review_pack`, `publish_pack`, `stage_knowledge`, `approve_profile`, `promote_profile` from the reviewer role | cards; human commands for approvals | `AGENT_INTERACTION_ARCH.md` H9 |
| The "note it and answer next time" rule for unanswered cards | refusal on submit | RV5 |
| `Review@1` | `Review@2` (adds `lead_checks`; findings carry staged file paths) | RV5 |

---

## 12. Work packages

Format as in `AGENT_INTERACTION_ARCH.md` section 13. Foundations H1 to H3 come first: this plan adds facts
(`check.ran`, `finding.refuted`, `reviewers.picked` with lenses) and kinds (`refute`, `critique`).

### Spikes

| Spike | Do | Record |
|---|---|---|
| SR1 | For each tool in 4.2: read its current licence file; run it offline in the sandbox image on a fixture; capture its SARIF or the output a converter needs; note version, digest, run time | A table in `PROGRESS.md`. A tool that fails any column leaves the catalogue |
| SR2 | Build the symbol index with universal-ctags on `pay-demo`, `ledger-demo` and `settle`; re-run the change-map cases G1 and G2 | Found or lost, and the run time |
| SR3 | Read the licence of the Opengrep rules repository and decide the engine | Engine choice |

### RV1. Checks (F4, 4.1 to 4.5)

- **Goal:** `checks:` in the profile, one executor, SARIF in, new-only, two routes.
- **Read first:** TECH_SPEC 12.2 and 12.2b; `ports/types_profile.go`; `adapters/profileyaml`;
  `adapters/rootlessrunner` (`policy.go`, `check.go`, `runner.go` promotion); `effects/verify_evidence.go`;
  `domain/verify/e2.go`.
- **Change:** `Profile.Checks`; `domain/sarif` (`Parse`, `Result`, `New`); a `check` effect that runs each
  matching check on base and head; `check.ran` and `finding.filed` (enforced) events; `policy.Checks.Enforce`;
  `run_check(check_id)`.
- **Tests first:** SARIF parse (bounds, malformed, unknown fields ignored); `New` (moved code is not new, changed
  line is new, base-only result dropped); glob selection; enforce matching; the executor with a fake sandbox.
- **Done when:** on the VM, a PR that adds a secret gets an E2 finding with no model call, and an old result on
  untouched code produces nothing.
- **Do not:** add a tool name to any Go file outside `deploy/checks`.

### RV2. The `checks` image

- **Goal:** the image of 4.2 with the tools that passed SR1, converters with golden tests, the warm job fetching
  databases and rules, digest on the allowlist.
- **Done when:** every catalogue row has a fixture change that triggers it and one that does not, both run
  offline.

### RV3. Lens and stack packs, card scope (5.1, 5.2)

- **Goal:** `Scope` and `Lens` on cards; `.heimdall/lenses/` and `.heimdall/rules/` read at the merge base;
  template packs in `deploy/lenses`; the setup PR copies them. Stack skills in `skills/stacks/`, and the
  brief's `skills` list from the changed files' globs.
- **Read first:** `domain/rulecard` (`card.go`, `rank.go`), `adapters/cardsfs`, `app/reviewbrief`,
  `deploy/cards/README.md`.
- **Tests first:** card parsing with scope; ranker treats scopes equally except the E3 rule (person and
  confirmed only); the skills list for a set of changed files.
- **Done when:** on a Python-only change the brief names `stack-python` and not `stack-go`, and the record shows
  the seat loaded it.

### RV4. Symbol index (4.7), after SR2

- **Goal:** the `symbols` job and `codegraph` reading it.
- **Done when:** the Go parser path and the regex taggers are deleted and the bench cases still pass.

### RV5. Routing, leads, complete answers (5.3 to 5.5)

- **Goal:** the `route` step, lenses in `reviewers.picked`, `leads` in the brief, `Review@2`, submit refusals.
- **Read first:** `engine/steps_pick.go`, `intake/reviewers.go`, `app/review`, `domain/finding/review.go`.
- **Tests first:** needed-lens computation; seat selection (holding seats, picks, `require_lens`, count); policy
  validation (uncovered lens, two people on a seat); submit refusals (missing answer, contradiction, confirmed
  lead without finding).
- **Done when:** live, a change with a migration gives exactly one seat the data lens and its leads, and a
  review that skips one is refused with the card named.

### RV6. One neutral skill, proof in any language (7.3, 7.4)

- **Goal:** the review skill as one page with no language in it; `how_to_test` from the profile; tools by kind.
- **Tests first:** the skill names only its kind's tools (existing test).
- **Done when:** a seat files an E1 repro on the Python fixture.

### RV7. Refute (section 6)

- **Goal:** kind `refute`, `Refutation@1`, the location check, `finding.refuted`, rendering.
- **Tests first:** the location rule (exists, not the finding's own lines); the derived "is refuted" predicate
  (later evidence reopens); who may refute (not a reporter).
- **Done when:** live, a planted false E4 finding is refuted with a citation and no card opens; a planted true
  one is not, and its card says so.

### RV8. Scope and budget (section 7)

- **Tests first:** scope classification from a diff and a graph; budget ordering.
- **Done when:** a true remark on untouched code appears under "Noticed" and blocks nothing; 30 findings post 10
  line comments.

### RV9. Activation path A1 to A6

- A1 and A2 can start with the foundations. A5 needs RV1 to RV3, RV5, RV7.
- **Done when:** the ablation table exists, on the production model.

### RV10. History mirror and `heimdall-mine` (4.6)

- **Tests first:** the miner on a synthetic history (support, confidence, the 30-file skip, merges ignored).
- **Done when:** on the fixture, a change that edits a migration without its schema file gets the lead.

### RV11. Learning extensions (section 9)

- **Tests first:** weights for lens cards and check rules; refutation record; dismissal categories.
- **Done when:** a rule whose leads were dropped five times is muted and listed.

**Order.** SR1 to SR3, RV1, RV2, RV3, RV5, RV6, RV7, RV8, RV9, then RV4, RV10, RV11. RV1 to RV3 are
independent of each other after the foundations and can run in parallel sessions.

---

## 13. How we know it works

Online, from the event log, per repository and per lens:

| Metric | Meaning |
|---|---|
| Tier mix | Findings at E1, E2, E3, E4 |
| Acted-on rate | Findings fixed, upheld or resolved / findings shown. Decided by a re-run, not by a model judge |
| Dismissals by reason | wrong, out of scope, trade-off, preference |
| Refute kill rate and overturn rate | What S4 removes, and how much of that was wrong |
| Lead conversion per rule | Confirmed / given |
| Human cards per PR, comments per PR | The attention we cost |
| Tokens and sandbox seconds per PR | Cost |

Offline: the bench of A4 to A6, then real history. The ship bars of TECH_SPEC 6.7 stay (recall at least 0.6,
precision at least 0.7, evidence rate at least 0.8), measured on the production model. The honesty rules of
`KNOWLEDGE_TOPOLOGY.md` 8.3 apply to this document: nothing in sections 4 to 7 has been measured by us.

---

## 14. Decisions for the owner

| # | Decision | Recommendation | Why |
|---|---|---|---|
| R1 | Seats with lens packs; template dev, security and infra seats by default | Yes | Every domain covered on day one; people upgrade seats |
| R2 | Checks as profile rows with a SARIF contract; no tool in server code | Yes | Language-agnostic by construction |
| R3 | Heimdall writes and owns its pattern rules; no third-party rule set is shipped | Yes | Licence, and rules that match our cards |
| R4 | Refute stage for unproven findings of medium and above | Yes | False positives are the top measured reason reviews are ignored |
| R5 | Enforced rules file findings with no model | Yes, list per repo in policy | Deterministic evidence first |
| R6 | One symbol index for all languages, replacing the Go parser path | Only if SR2 keeps the bench cases | No quality loss for the sake of tidiness |
| R7 | Co-change mining as a lead source | Yes, later phase | Cheap, language-blind; conversion will be measured |
| R8 | One model for every stage | Yes | No model chains without an observed need |
| R9 | Line comment cap | 10 per round, in policy | A budget forces ranking |
| R10 | Publish the bench and its failures | Yes | The category's numbers are not trusted |

---

## 15. Non-goals

- No auto-fix, no auto-merge, no model deciding the gate.
- No specialist agent type per lens, no prompt per language.
- No scripted debate rounds. Seats may talk; the server does not stage it.
- No style review unless the repo adds its linter as a check.
- No reviewer access to the network, a checkout or credentials.
- No single headline score.

---

## 16. Not verified

- Every tool in 4.2 on our runner: image, version, licence for inclusion, offline rules, SARIF flag, run time.
- The symbol index's quality against the current Go path.
- That the production model triages leads, answers lens cards, loads the right skills, and refutes with correct citations.
- That co-change leads are worth their tokens.
- That context improves review beyond one synthetic PR. Section 8 exists to find out.
- All vendor numbers in section 2.
- `make fast` and `make verify` were not run for this document: it changes no code.

---

## 17. Sources

Research helpers, Nov 14, 2026. (F) fetched, (S) summary only.

- "Is Agentic Code Review Helpful? Mining Developers' Feedback to CodeRabbit Reviews in the Wild", arXiv, Jul 2026 (F)
- Cursor, "Building Bugbot", "Bugbot learning"; Ellipsis docs; Anthropic, Claude Code Review (F; their numbers are vendor claims)
- SWR-Bench, arXiv 2509.01494; CR-Bench, arXiv 2603.11078; Refute-or-Promote, arXiv 2604.19049 (F)
- IRIS, arXiv 2405.17238; LLM as SAST false-positive filter, arXiv 2506.16899 (F)
- Semgrep licensing, docs.semgrep.dev/licensing; Semgrep CLI reference (F); Opengrep README (F)
- Trivy air-gap docs, trivy.dev (F); the March 2026 Trivy incident (S)
- gitleaks, Grype, Checkov, hadolint, gosec READMEs and docs (F)
- GitHub code scanning SARIF support, `partialFingerprints` (F); microsoft/sarif-tools (F); reviewdog (F)
- Zimmermann, Weissgerber, Diehl, Zeller, "Mining Version Histories to Guide Software Changes", IEEE TSE 2005 (S)
- Qodo Rule Miner docs (S); Getafix, arXiv 1902.06111 (S)
- Earlier sources: `REVIEW_MOAT.md` section 6, `KNOWLEDGE_TOPOLOGY.md` section 12
