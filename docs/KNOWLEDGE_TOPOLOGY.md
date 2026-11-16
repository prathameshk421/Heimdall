# The shared brain on the VM: scopes, stores, and where Heimdall can cook

Status: exploration and plan. Nothing here is implemented. Written Nov 10, 2026.
Companion to `docs/CONTEXT_CAPTURE_PLAN.md` (the capture plan) and `docs/REVIEW_MOAT.md` (what exists).
Read with `deploy/compose.yaml`, `deploy/graphiti/config.yaml`, `internal/app/knowledge`,
`internal/app/reviewbrief`, and `internal/app/bundle`.

This document answers three questions:

1. **Where does the knowledge live on the VM**, next to Graphiti, and how do the four scopes compose?
2. **What knowledge actually helps a review and a test**, with a schema for each kind?
3. **What do CodeRabbit, Bugbot, Greptile, Qodo, Graphite and the test tools do, what do they get wrong, and
   where can we be better rather than merely different?**

The short answer to (3): the market has gotten good at *repo-level* context and *reaction-based* learning. It has
not solved *person-level* knowledge, *business-rule* judgement, *out-of-scope* noise, or *oracle-quality* tests.
That is the gap we can own.

---

## 0. The thesis in one paragraph

The VM is the **shared brain**, the laptop is the **capture edge**. The brain holds four stores — Postgres for
identity, events, provenance and audit; the repo for the truth about the codebase; Graphiti for a rebuildable
semantic index; and a private cards volume for the person. Knowledge is scoped four ways — codebase, department,
role, person — and composed by one deterministic compiler with a token budget and provenance on every item.
Reviews need business rules, incident memory, conventions and compatibility. Tests need regression seeds, infra
maps, bug-class heatmaps and oracle knowledge. The market has repo context and reaction learning; nobody has the
person, the business rule, the scope gate, or the oracle gate. Those four are the cook.

---

## 1. The four scopes

The user asked for role-specific, person-specific, department-specific and codebase-specific knowledge. They are
not four copies of the same thing. Each answers a different question, has a different author, and carries a
different trust level.

| Scope | Question it answers | Store | Example | Who writes it | Confirmed by | Trust |
|---|---|---|---|---|---|---|
| **Codebase** | What is true about this system? | repo + Graphiti | "The refund cap counts refunds already made." "Wire format v2; v1 clients until Q3." | developer + their agent | human merge | confirmed on merge; E3 when scoped + owned |
| **Department / team** | What does this group care about, and how does it work? | repo `.heimdall/teams/<team>.md` + Graphiti group `team:<team>` | "Payments requires a rollback for every migration." "Infra is EKS 1.30; no host networking." | team lead + agent | human merge | confirmed on merge; E3 |
| **Role** | What does this job need to check? | versioned skill + repo `.heimdall/roles/<role>.md` | "A reviewer must check the contract surface and removed symbols." "A test author must not mock the seam." | Heimdall (skill) + team (domain specifics) | code review + release | system, versioned; never E3 by itself |
| **Person** | What does this individual know, prefer, block on, not know? | cards + POV (private volume) | "Bob blocks a migration without a rollback." "Ana owns infra; bob does not review manifests." | the person + their agent | the person (`cards confirm`) | private; E4 unless confirmed; a confirmed card can reach E3 |

### 1.1 Composition rule

The compiler never merges scopes silently. It applies two different precedence orders, because **truth** and
**force** are not the same thing.

- **Truth precedence:** codebase (merged) > department (merged) > role (system) > person (card) > POV.
  The codebase is what the team agreed is true at the base SHA.
- **Force precedence:** person (confirmed card) ≈ codebase (confirmed fact) > department > role > POV, with
  **specific beats general** on a tie.
  A confirmed card can block for that person; a confirmed codebase invariant can block for everyone. POV never
  blocks.

When two confirmed facts contradict, the compiler shows both with provenance and files an E4. A human resolves it
by editing one. The model never picks.

### 1.2 Why four scopes and not one

- A single "repo knowledge" store cannot express "this is true for payments but not for the ledger".
- A single "person" store cannot express a team rule that outlives the person.
- A single "role" store cannot express the team's domain specifics.
- The scopes let the compiler answer: *what is true here, what does this team require, what does this job check,
  and what does this person enforce?* Four different questions, four different answers.

---

## 2. VM topology: where the brain lives

The current stack (`deploy/compose.yaml`) is already the right shape. We are not adding a service; we are
**promoting Postgres to the knowledge catalog** and **adding a private cards volume**.

### 2.1 Services and what each owns

| Service | Network | Publishes | Owns | Why here |
|---|---|---|---|---|
| `heimdall-server` | core, memory, switch | `127.0.0.1:8080` | the compiler, MCP, intake, gate | it is the only component that may read all stores |
| `postgres` | core | none | identity, events, tasks, **knowledge catalog**, provenance, audit, pack cache, fact outcomes | transactional truth belongs in Postgres |
| `falkordb` | memory | none | the graph bytes | Graphiti's store |
| `graphiti` | memory | none | episodes, entities, edges, temporal validity, semantic search | the rebuildable index |
| `embeddings` | memory | none | vectors (bge-small, 384d) | local, no model provider needed |
| `switch-forward` | shares server netns | none | loopback relay to Switch | existing |
| `cards_data` (volume) | mounted into server | none | cards + POV, mode 0700 | private, inspectable, never in Graphiti |
| `repo_cache` (volume, optional) | mounted into server | none | tarballs/bare repos by SHA | avoids refetching a SHA for every brief |

Networks stay as they are: `core` (server ↔ Postgres), `memory` (server ↔ Graphiti ↔ FalkorDB ↔ embeddings),
`switch` (external). **No new network and no new service.** The cards volume is the only new mount.

### 2.2 What each store holds

#### Postgres — the catalog and the audit

Graphiti is good at semantic search and bad at transactional truth. Postgres is the opposite. So:

```
knowledge_facts
  id, repo, component, kind, slug, claim, scope_json, source_ref, source_sha,
  owner, trust, valid_from, valid_until, review_by, fingerprint, created_at

knowledge_sources
  fact_id, source_kind, source_ref, sha, url

fact_citations
  fact_id, change_id, finding_id, agent, cited_at, outcome   -- outcome: upheld|dismissed|resolved|ignored

context_packs
  id, change_id, role, person, pack_hash, budget, omitted_json, created_at

context_queries
  id, task_id, agent, question, entities, result_refs, created_at

cards_index
  person, repo, name, kind, scope_json, text_hash, status, confirmed_at, muted, last_cited

pov
  person, repo, section, text, updated_at        -- or a file; see 2.3

component_map
  repo, sha, yaml_hash, parsed_json

ingest_runs
  repo, sha, files, facts, errors, started_at, finished_at
```

Why Postgres and not Graphiti for this:

- `fact_citations` and `fact_outcomes` are the learning loop. They need joins, aggregates and idempotency.
- `context_packs` is a cache keyed by `(change, role, person, head_sha)`. A cache in a graph DB is a mistake.
- `ingest_runs` is operational truth: did the merge get indexed, and what failed.
- Graphiti can be dropped and rebuilt; Postgres cannot. So the catalog lives in Postgres, and Graphiti is derived.

#### Graphiti / FalkorDB — the rebuildable index

One episode per fact, not per chunk:

- Entity types: `Component`, `Discipline`, `Invariant`, `Incident`, `Decision`, `Person`, `Requirement`,
  `Procedure`, `Contract`.
- Edges: `AFFECTS`, `CAUSED`, `SUPERSEDES`, `OWNS`, `REQUIRES`, `VIOLATES`.
- Keys: `repo:<owner/name>#<kind>:<slug>`.
- Groups: `repo:<owner/name>`, `team:<team>`, `shared`. Never `person:<name>` — personal facts do not enter.
- Temporal: `valid_at` from the source date, `invalid_at` when superseded or the ticket closes "won't do".
- Trust: `observed` (model extraction) or `confirmed` (human merge). The durable `ports.EpisodeSourceTrust`
  ledger overrides Graphiti's own labels.

What Graphiti is *for*: "find the incident most similar to this diff", "what decisions touch this component",
"what changed in the graph since last quarter". What it is *not* for: the record, the audit, the personal.

#### The repo — the truth about the codebase

```
.heimdall/
  profile.yaml          # sandbox contract (D98/D153)
  components.yaml       # components + disciplines (new)
  knowledge/            # one durable fact per file (new)
    refund-cap.md
    inc-031-bank-drop.md
    wire-format-v2.md
  teams/
    payments.md         # department scope (new)
  roles/
    reviewer.md         # role scope, optional (new)
docs/                   # narrative, ADRs, runbooks (existing)
```

Read at the **base SHA** for review and the **merge SHA** for ingestion. Never the PR head. This is the same rule
as policy, and it is what stops a PR from editing its way past a check.

#### The cards volume — the person

```
<cards_dir>/
  <repo>/<person>/
    profile.md          # POV: how I work, what I optimize, what I refuse, what I don't know
    refund-cap.md       # a rule card
    inc-031.md          # an incident card
```

Mode `0700`, owned by the server user. Read by the compiler for the brief; never indexed in Graphiti; never
logged; never returned to another person's agent. The CLI writes locally and pushes; the server only reads.

### 2.3 Data flow

```
developer's agent (laptop)                         VM
  |  heimdall context init/ask/lint/propose         |
  |  heimdall cards scan/prompt/check/push          |
  v                                                 v
repo files + cards  --(PR / push)-->  repo (git) + cards_data volume
                                          |
                    server: on merge/poll -> ingest -> Postgres catalog + Graphiti episodes
                                          |
reviewer agent  --(MCP get_task)-->  compiler -> ContextPack -> brief
test author     --(MCP get_task)-->  compiler -> ContextPack -> test plan
resolver        --(MCP get_task)-->  compiler -> ContextPack -> prior decisions
```

### 2.4 Ports and exposure

| Port | What | Exposure |
|---|---|---|
| `127.0.0.1:8080` | heimdall-server (`/mcp`, `/healthz`, `/cards`, report) | loopback only (existing) |
| `127.0.0.1:8081` | optional read-only context inspector | off by default |
| none | Postgres, FalkorDB, Graphiti, embeddings | container networks only (existing) |

The inspector is optional and read-only: it renders a `ContextPack` for a human, so an operator can see exactly
what a reviewer saw and why. It must never expose card text to a non-owner; it is a debugging tool, not a product.

### 2.5 Failure modes and what the brief says

| Failure | Effect | Mitigation |
|---|---|---|
| Graphiti down | semantic search and E3 memory unavailable | brief says "Team memory unavailable: carry on"; findings fall to E4 |
| Postgres down | no task, no audit | server unhealthy; no review starts |
| cards volume missing | no personal context | brief says "Personal cards unavailable: use read_card" |
| repo fetch fails | no change map, no repo facts | brief says "Code graph unavailable: use read_code" |
| compiler too slow | deadline pressure | cache by `(change, role, person, head_sha)`; per-source deadlines (existing) |
| stale pack after a push | the reviewer sees the old head | invalidate by head SHA; recompile on `get_task` |
| card text in a log | privacy breach | never log card/POV text; log only the ref and the byte count |

---

## 3. Review knowledge: what actually helps

The user named four kinds: business rules, what broke before and how, coding styles, tech stack and compatibility.
Each becomes a first-class kind with a schema, a retrieval rule, and an evidence tier.

### 3.1 Business rules / domain invariants

**What it is.** The rules of the business that the code must preserve: money movement, caps, tenancy, consent,
tax, SLAs, audit. The things a generic reviewer cannot know and a linter cannot check.

**Schema** (repo knowledge, `kind: invariant` or `decision`):

```yaml
---
id: refund-cap
kind: invariant
components: [refunds]
applies_to: ["internal/refunds/**"]
symbols: ["RefundService.Create"]
disciplines: [backend, money]
audience: [reviewer, test-author]
owner: bob
source: ADR-004
valid_from: 2026-11-08
review_by: 2027-02-01
---
A refund cannot exceed the captured amount minus refunds already made.
Two partial refunds of 60 on a payment of 100 must not both succeed.
The bank drops lines above 500 without an error, so this cap is the only guard.
```

**Retrieval.** Component + symbol + contract surface + added text. Confirmed first.

**Evidence.** E3 when confirmed, scoped and owned; otherwise E4.

**Why it wins.** CodeRabbit's field study found 43.3% of rejected comments were false positives. A large share of
those are "technically true, business-wrong": the model flags a pattern that violates no business rule and no
test. A business-rule gate flips the judgement before the comment is posted.

**How to check it deterministically where possible.** A cap, a tenant boundary, a consent flag and a money
movement often have a checkable shape: a comparison, a scope filter, a boolean guard. Express the check as a
small predicate over the AST or the diff where it is cheap; fall back to the model only for the semantic residue.

### 3.2 Past breakages / incident memory

**What it is.** What broke, the cause, the fix, the rule that came out of it, and what to look for to prevent a
repeat. This is the single highest-value repo knowledge type, because a generic reviewer has no memory of the
last outage.

**Schema** (repo knowledge, `kind: incident`):

```yaml
---
id: inc-031-bank-drop
kind: incident
components: [refunds, ledger]
applies_to: ["internal/refunds/**", "internal/ledger/**"]
symbols: ["RefundService.Create", "ledger.Batch.Append"]
disciplines: [backend, money, ops]
audience: [reviewer, test-author]
owner: bob
source: INC-031
valid_from: 2026-09-14
---
The bank silently drops batch lines above 500. INC-031 lost 1,200 refunds in one night.
Cause: the batch builder had no cap and the bank returned 200 for a truncated batch.
Rule: every batch is capped and the count is asserted before send.
Look for: a new batch call without a cap, a size derived from input length, a 200 treated as success.
Regression seed: 501 lines; expect a split into 2 batches, not one truncated send.
```

**Retrieval.** Component + symbol + hot file + semantic similarity to the incident's cause diff.

**Evidence.** E3 when confirmed; E4 otherwise. A confirmed incident that names the exact shape is strong.

**Innovation — incident fingerprint.** Embed the incident's *cause diff* (the code that broke) and the *fix diff*.
At review time, retrieve the nearest incident and ask a yes/no: "is this change the same shape as the cause?"
If yes, cite the incident. This is "have we been here before?" as a retrieval primitive, and no hosted tool has it
because no hosted tool has the incident's code shape plus the person who lived it.

### 3.3 Coding styles / conventions

**What it is.** The team's conventions that a linter does not enforce: naming, error wrapping, logging, test
structure, API shape, file layout. The ones that cause "valid but misaligned" rejections.

**Schema** (repo knowledge, `kind: convention`):

```yaml
---
id: error-wrapping
kind: convention
components: [payments, refunds, ledger]
applies_to: ["internal/**"]
disciplines: [backend]
audience: [reviewer, test-author]
owner: approver
source: docs/CODING_STANDARDS.md#6
valid_from: 2026-11-04
---
Wrap errors with the operation and `%w`, never the raw error and never a string-only message.
A caller must be able to branch with `errors.Is` / `errors.As`.
```

**Retrieval.** Component + path + added text.

**Evidence.** E4, never blocks. A convention is a preference unless it is an invariant.

**Why it wins.** 42% of CodeRabbit's rejections were "valid but misaligned with coding practices" (23.7% intended
trade-off, 18.3% personal preference). The fix is not a smarter model; it is the team's own convention, written
down, versioned, scoped, with the reason. CodeRabbit has path instructions and Code Guidelines; Bugbot has
`BUGBOT.md`. Heimdall can make them first-class, per component and per person, with freshness and provenance.

**Innovation — convention drift, not style guide.** A static style guide is stale by definition. Instead, learn the
*accepted* patterns for a component from merged PRs (e.g., "handlers in `internal/api` map domain errors in one
place"), and flag a change only when it drifts from that pattern in a statistically unusual way. That is a
different signal from "this differs from a generic rule", and it is what a senior reviewer actually does.

### 3.4 Tech stack and compatibility

**What it is.** Languages, frameworks, versions, pinned deps, supported clients, deprecation windows, platform
constraints, env vars, feature flags, contracts.

**Schema** (deterministic + repo knowledge, `kind: external_contract` or `decision`):

```yaml
---
id: wire-format-v2
kind: external_contract
components: [payments]
applies_to: ["api/**", "internal/wire/**"]
disciplines: [backend, product]
audience: [reviewer, test-author]
owner: eli
source: ADR-009
valid_from: 2026-06-01
review_by: 2027-01-01
---
The wire format is v2. v1 clients exist until 2027-Q3. Changes are additive only:
new optional fields are fine; renames, removals and type changes are breaking.
```

**Retrieval.** Manifest diff + import graph + contract strings + OpenAPI/proto diff.

**Evidence.** E2 (a tool result: `oasdiff`, `buf breaking`, a lockfile check) or E3 (a confirmed decision).

**Why it wins.** "A change to an API contract might break three downstream consumers" is the canonical diff-only
failure. CodeRabbit queries library docs on the web; a **compatibility ledger** is stronger: a machine-readable
table of supported versions/clients with dates, checked against the diff.

**Innovation — compatibility ledger.** For each component, a table of:
`client | version | supported_until | breaking_change_policy | evidence`. A diff that adds a call to an API above
the minimum supported version, or renames a field, is checked against the ledger deterministically. The model
explains; the ledger decides.

### 3.5 Ownership and trust (supporting)

**What it is.** Who owns what, who to ask, who trusts whom for what.

**Store.** `CODEOWNERS` + policy `people`/`approvers` + cards `kind: ownership`.

**Use.** Routing, severity, "ask ana for infra". Never blocks; it decides who should look.

**Why it matters.** It is the difference between a comment on a stranger's code and a comment from someone whose
judgement that team trusts. It also powers the E3 owner check for cards and repo facts.

### 3.6 Decision log (supporting)

**What it is.** A choice and its reason, with a date and a validity window.

**Store.** Repo knowledge `kind: decision`, Graphiti `Decision`.

**Use.** Stop re-litigating a settled decision; catch a change that violates it.

**Evidence.** E3 when confirmed. This is the fact class that most often turns a "nit" into a "block".

---

## 4. Test knowledge: what actually helps

The user named three: what broke previously, what infra and how it is used, what bugs have been reported most.
Each becomes a knowledge kind with a retrieval rule and a test-side use.

### 4.1 Regression seeds (from past breakages)

**What it is.** The bug, the input that triggered it, the oracle that would have caught it, and the fix. A
"regression seed" is the pair `(input, expected behaviour)` that a new test must cover.

**Schema** (repo knowledge, `kind: incident`, plus a `regression` block):

```yaml
---
id: inc-031-regression
kind: incident
components: [refunds, ledger]
applies_to: ["internal/refunds/**", "internal/ledger/**"]
disciplines: [backend, money]
audience: [test-author, reviewer]
owner: bob
source: INC-031
valid_from: 2026-09-14
---
regression:
  input: "501 batch lines"
  expect: "two batches, each <= 500, both acknowledged"
  fails_on: "the parent of fix commit 4fa91c2"
  passes_on: "4fa91c2"
```

**Use.** The test author gets the top seeds for the touched component and must cover them. The harness runs the
new test against the parent commit: it must fail there and pass at head. That is a real oracle, not a coverage
number.

**Why it wins.** Every review tool bolts on test suggestions; every test tool ignores review context. Nobody
connects "this broke before" to "write this test". Heimdall can, because it has the event log, the fix commit and
the incident note in one place.

### 4.2 Infra and how it is used

**What it is.** Services, databases, queues, caches, external APIs, their test doubles, the seam, the network
rules, the fixtures.

**Schema** (`.heimdall/profile.yaml` + repo knowledge `kind: procedure`):

```yaml
---
id: test-infra-refunds
kind: procedure
components: [refunds, ledger]
applies_to: ["tests/**", "internal/refunds/**"]
disciplines: [backend, infra]
audience: [test-author]
owner: ana
source: .heimdall/profile.yaml
---
Postgres 16 with a throwaway DB and no egress. The payment provider is a local fake at /fake-pay.
Never mock the seam (the HTTP client); use the fake. Fixtures: two payments, one partial refund.
The ledger batch cap is 500; the fake bank rejects a truncated batch with 400.
```

**Use.** The test author knows what to run, what to mock, what not to mock, and which infra is real in the
sandbox. The compiler emits a **test plan**: services needed, fixtures, seam, existing tests that cover the
touched symbols, gaps to fill.

**Why it wins.** AI tests that mock everything and assert nothing are the norm (see 4.4). The seam rule plus a
real service container force behaviour, and the test plan stops the agent from rewriting the whole file.

### 4.3 Bug-class heatmap

**What it is.** The bug classes that recur in this component, ranked by frequency × severity, with example inputs.
Derived from the event log (findings), GitHub issues, incident notes and fix commits.

**Schema** (derived, cached in Postgres, rendered into the brief):

```json
{
  "component": "refunds",
  "classes": [
    {"class": "cap-not-cumulative", "count": 4, "severity": "high", "last_seen": "2026-10-30",
     "example": "two partial refunds of 60 on 100", "test_hint": "sum prior refunds, not the request"},
    {"class": "batch-truncation", "count": 2, "severity": "critical", "last_seen": "2026-09-14",
     "example": "501 lines", "test_hint": "assert the split count"}
  ]
}
```

**Use.** The reviewer checks one hypothesis per class. The test author writes one test per class. A cheap model
with three focused hypotheses beats a cheap model told to "review everything".

**Why it wins.** It converts history into a prior. It is the test-side twin of incident memory, and it makes the
"most-reported bugs" ask actionable.

**Metric.** Does the heatmap predict the next bug? Track the hit rate: of the bugs found in the next N changes,
how many were in the top 3 classes? If the heatmap is wrong, prune it.

### 4.4 Oracle knowledge (the biggest test gap)

**What it is.** How to decide pass/fail for this component: exact output, property, metamorphic relation,
differential vs a reference, invariant.

**The market flaw, with evidence.** A 2026 study of 86,156 changes to test files by five coding agents found that
**80.2% had weak or no explicit oracle signals**. Many tests confirmed the code ran while checking little or
nothing about its output. Coverage is not oracle quality.

**Heimdall's rule — the oracle ladder.** Every generated test must declare its oracle and justify why a stronger
one is not available:

| Rung | Oracle | When | Example |
|---|---|---|---|
| 1 | Exact output | deterministic, small output | `assert refunds == []` |
| 2 | Property / invariant | a rule must hold for all inputs | "sum(refunds) <= captured" |
| 3 | Metamorphic | the relation between input and output is known | "doubling the input doubles the fee" |
| 4 | Differential | a reference implementation exists | old vs new, or two algorithms |
| 5 | Regression seed | a past bug gives the input and the expected behaviour | the `regression` block above |
| 6 | No oracle | never accepted | — |

A test with rung 6 is rejected by the harness, not by a prompt. This is a deterministic gate on the weakest link
in AI testing.

### 4.5 Flaky/fragile test memory

**What it is.** Tests that failed intermittently, the cause, the fix, and the patterns that make this repo flaky
(sleeps, shared state, wall-clock time, network, ordering).

**Schema.** CI history + repo knowledge `kind: incident`.

**Use.** The test author sees the flaky patterns and must avoid them; a change that touches a flaky area is
flagged for stabilization, not new coverage.

**Why it wins.** A generated test that is flaky is worse than no test: it teaches the team to ignore red. The
memory of *why* this repo's tests flake is not in any public tool.

---

## 5. The context compiler (one object, four scopes)

The compiler is a package in the server (`internal/app/context`, new), not a service. It takes a
`(role, person, change)` and returns a `ContextPack`.

```
ContextPack Compile(role, person, change):
  touched   = changed files + dependent files (code graph)
  comps     = resolve(touched, components.yaml) + graph components
  discs     = union(comps.disciplines, person.disciplines, team.disciplines)
  audience  = role + person

  candidates =
      derived(touched)                                  # change map, ownership, hot files
    + codebaseFacts(repo, base, comps, discs, audience) # confirmed first, then observed
    + teamFacts(team, comps, discs, audience)           # department scope
    + roleRules(role)                                   # versioned skill
    + personalCards(person, repo, comps, discs)         # ranked, outcome-weighted
    + pov(person, comps, discs)                         # global + matching sections
    + priorFindings(repo, touched)
    + record(person.agent)

  ranked = sort(candidates, by:
      can_block desc, scope_specificity desc, trust desc, freshness desc, outcome_weight desc)

  return budget(ranked, roleCap, omit=lowest)           # never silently truncate
```

The full ranking, budget and eviction rules are in `docs/CONTEXT_CAPTURE_PLAN.md` §7.3–7.4. The new part here is
the **scope dimension**: each candidate carries `scope ∈ {codebase, team, role, person}` and the two precedence
orders from §1.1.

### 5.1 Where the compiler reads

| Source | Read | Deadline | On failure |
|---|---|---|---|
| Postgres catalog | facts, citations, outcomes, component map | fast | section empty, logged |
| Graphiti | semantic search by component/entities/as-of | 8s | section says unavailable |
| Repo at base | code graph, knowledge files, components | 20s | section says unavailable |
| Cards volume | cards + POV | fast | section says unavailable |
| Event log | prior findings, record | fast | section empty |

Each section fails alone. A failed section never costs the task. This is already the design in
`internal/app/reviewbrief`; the compiler generalises it.

### 5.2 The pack is the same object in every surface

- `get_task` preloads it.
- `read_context` lets an agent ask for more.
- `read_pov` returns the person's POV, scoped.
- `ask_knowledge` is the free-text search over the graph.
- `heimdall context status` is the human view: confirmed, observed, stale, omitted.
- The optional inspector renders it for an operator.

One compiler, many renderings. No second retrieval path to drift.

---

## 6. What the market does, and what it gets wrong

The numbers below are from public sources with different methodologies. They disagree with each other, which is
itself a finding (see 6.3). Treat every vendor number as a claim, not a fact.

### 6.1 The players

| Product | What it actually does | Strength | Documented weakness |
|---|---|---|---|
| **CodeRabbit** | Code graph, 25+ linters/SAST, knowledge base from Jira/GitHub issues, Learnings from PR comments, path instructions, code guidelines, web queries, plan/triage/explain/security | Broadest platform coverage; top F1 on Martian's Feb 2026 cut (51.2%), precision 49.2% | Independent field study of 31,073 comments: **56.3% rejected**, 43.3% false positives, 10% out of scope, 4.7% redundant; 42% "valid but misaligned" (23.7% intended trade-off, 18.3% preference); 10.3% of FPs from missing cross-file visibility |
| **Cursor Bugbot** | Composer 2.5; learned rules from reactions, replies and human comments (110k repos, 44k rules); `BUGBOT.md`; per-repo config; effort levels; incremental review; `/review` pre-push; autofix | Vendor-reported **78.13% resolution rate**, ~90s reviews, learns from real signals, pre-push | Resolution rate is vendor-graded with an LLM judge; rules are repo/org-level, not person; no test generation; no deterministic evidence gate; a rule learned in one area can be wrong in another |
| **Greptile** | Repo graph, `.greptile/` config, 82% recall claim, 60.8% F1 / 76.2% precision on Martian Jul 2026 | High recall and precision on one benchmark | No false-positive count published; benchmark-contested; no outcome learning; no tests |
| **Graphite Diamond** | Diff/stack-aware, tuned to team history | Lowest noise: <5% negative, 3.5% downvoted | Caught only 6% of bugs in a July 2025 benchmark; low recall; GitHub-only |
| **Qodo** | Rules-based, ticket compliance, multi-agent, Qodo Gen tests, 60.1% F1 | Review + tests in one, governance | Heavy config, stale rules, needs a platform owner; governance-first, not bug-first |
| **Diffblue Cover** | RL-based Java unit test generation | 80.7% coverage, compiles | Java only; coverage is not oracle quality; implementation-coupled tests |
| **Qodo Gen / Copilot / Claude Code** | Behaviour-based test drafts | Cheap, fast | 80.2% of AI test changes have weak/no oracle; happy path; mocks everything |

### 6.2 The flaws, grouped

**A. The person is missing.** Every tool learns per repo or per org. None models one engineer's rules, incidents,
preferences, unknowns. Bugbot's learned rules are org/repo-scoped. CodeRabbit's Learnings are per-repo. A rule that
is true for Bob's area leaks into Ana's review, or is invisible because it lives only in Bob's head.

**B. "Valid but misaligned" is 42% of rejections.** The model is often *right* and still useless, because it does
not know the team's trade-off or preference. More model quality does not fix this; the team's own written,
versioned, scoped conventions do.

**C. Out-of-scope is 10%.** No tool checks "is this in the ticket's scope?" The reviewer has the ticket and the
acceptance criteria. A deterministic scope gate can suppress the comment before it is posted.

**D. False positives from missing cross-file visibility are 10.3%.** The code graph exists; the fix is to force
the reviewer to read the dependent file before claiming, and to require a refute step with evidence.

**E. Resolution rate is vendor-graded and gameable.** "Addressed by merge" is not "correct". An LLM judge can be
fooled by a cosmetic edit. Heimdall's E1/E2/E3 tiers are auditable: a test that fails, a tool that re-runs, a
confirmed fact that re-queries. Publish the tier mix, not a single resolution number.

**F. Learning promotes local rules globally.** A rule learned from one repo's reactions can be wrong elsewhere,
and a rule can be promoted by a single enthusiastic reviewer. Heimdall's card is per person + repo + component,
confirmed by the owner, and muted by outcomes.

**G. Tests are the weak half everywhere.** Review tools bolt on test suggestions; test tools ignore review
context. Nobody connects "this bug broke before" to "write this test", or "this test has no oracle" to "reject it".

**H. Benchmarks are unstable and vendor-contested.** The same tool ranks 51.2% F1 and 33.0% F1 in two 2026
benchmarks. Greptile ranks 60.8% and 36.9%. Vendor posts cherry-pick. Gold labels are noisy. "LLM judge" is not a
measurement. This is a trust problem the whole category has, and an honest ablation is a differentiator.

**I. Context is a bag, not a budget.** Tools add context; few rank, evict, or provenance it. Long-context research
shows performance degrades when relevant information sits in the middle, and attention bias favors early and nearby
tokens. More context can make a reviewer worse.

**J. No one closes the loop from test failure to review finding to rule.** The event log can. A test that fails on
a finding, a fix that lands, a dismissal with a reason, a card that is confirmed: one loop, one record.

### 6.3 The benchmark problem, stated honestly

| Benchmark | Date | Claim | Caveat |
|---|---|---|---|
| Martian Code Review Bench | Feb 2026 | CodeRabbit 51.2% F1, 49.2% precision | Independent lab, 200k+ PRs, daily updates; still one methodology |
| Martian Code Review Bench | Jul 2026 | Greptile 60.8% F1, 76.2% precision; CodeRabbit 57.5% | Same bench, different month, different winner |
| Entelligence | 2026 | Codex 64.5%, Bugbot 39.4%, Greptile 36.9%, CodeRabbit 33.0% | Different gold set; rankings invert |
| Cursor (vendor) | Apr 2026 | Bugbot 78.13% resolution, CodeRabbit 48.96% | Vendor-graded, LLM judge |
| CodeRabbit field study | Jul 2026 | 56.3% of comments rejected | Independent, 31k comments, but one tool and one period |

The lesson: **do not chase a single F1**. Report per-source knowledge gain on a held-out set, with the
methodology and the failure cases. That is more credible than a leaderboard, and it is the only number that tells
you whether *your* knowledge helped *your* team.

---

## 7. Where Heimdall can cook

Twelve ideas, each with the flaw it attacks, the mechanism, the cost, and how to prove it. Ordered by
differentiation × feasibility.

### 7.1 Person-scoped reviewer packs (the moat)

**Attacks:** A (the person is missing).
**Mechanism:** each reviewer agent is one person; the compiler builds a pack from their cards, POV, disciplines and
confirmed rules, scoped to the change's components. Two reviewers on the same PR give different lenses.
**Cost:** low; the cards and compiler already exist.
**Proof:** a review of a bug only that person knew about, with the card written *before* the bug, found by their
reviewer and not by the others.

### 7.2 Business-rule gate

**Attacks:** B, D (business-wrong and cross-file false positives).
**Mechanism:** a confirmed `invariant`/`decision` is checked against the diff before the model. Deterministic
where the rule has a checkable shape (a cap, a tenant filter, a guard); model only for the semantic residue. A
finding that violates no rule and no test is downgraded to E4.
**Cost:** medium; needs the rule schema and a small predicate layer.
**Proof:** the refund-cap bug: the gate blocks the second refund without a model call, and the generic reviewer
does not.

### 7.3 Out-of-scope gate

**Attacks:** C (10% of rejections).
**Mechanism:** compare the finding's location and claim to the ticket's acceptance criteria and the PR's stated
intent. If it is outside both, mark `out_of_scope` and never block. The gate already has `compat/ops/security`
for outside-the-diff; add the scope dimension.
**Cost:** low; the ticket and criteria are already in the task envelope.
**Proof:** the bench's clean control PR: the out-of-scope nit is suppressed, and the real finding is not.

### 7.4 Incident fingerprint

**Attacks:** A, B (the last outage).
**Mechanism:** embed the incident's cause diff; at review time retrieve the nearest incident and ask a yes/no:
"is this the same shape?" If yes, cite the incident as E3.
**Cost:** medium; needs the incident corpus and an embedding of diffs.
**Proof:** replay a fix commit's parent; the fingerprint retrieves the incident and the reviewer finds the bug
that the generic reviewer misses.

### 7.5 Regression seeds for tests

**Attacks:** G (tests are the weak half).
**Mechanism:** from fix commits and reverted PRs, produce `(input, expected behaviour)` seeds. The test author must
cover the top seeds for the touched component. The harness runs the test against the parent commit: it must fail
there and pass at head.
**Cost:** medium; mining fix commits is deterministic.
**Proof:** a seed from a real fix; the generated test fails on the parent and passes on the fix.

### 7.6 Oracle ladder + no-oracle refusal

**Attacks:** G (80.2% weak or no oracle).
**Mechanism:** every test declares its oracle rung (exact, property, metamorphic, differential, regression seed).
Rung 6 (no oracle) is rejected by the harness, not by a prompt.
**Cost:** low; a schema field and a validator.
**Proof:** a generated happy-path test with `assert.NoError` is refused; a property test is accepted.

### 7.7 Mutation-gated test acceptance

**Attacks:** G (coverage is not quality).
**Mechanism:** a generated test must kill at least one mutant in the changed code. Generate mutants only for the
changed lines (cheap, test-aware, per the 2026 ESEM work), not the whole repo. Mutation score is the acceptance
gate, not a dashboard number.
**Cost:** medium; needs a mutation runner in the sandbox.
**Proof:** a test that passes but kills no mutant is refused; a test that kills the planted bug's mutant is
accepted.

### 7.8 Bug-class heatmap

**Attacks:** B, G (where to look, what to test).
**Mechanism:** per component, the top failure modes with counts and examples. The reviewer checks one hypothesis
per class; the test author writes one test per class.
**Cost:** low; derived from the event log.
**Proof:** the heatmap predicts the next bug: of the next N bugs, how many were in the top 3 classes?

### 7.9 Compatibility ledger

**Attacks:** D (breaks three services away).
**Mechanism:** a machine-readable table of supported versions/clients with dates and breaking-change policy;
check the diff against it (`oasdiff`, `buf breaking`, lockfile, API version). Deterministic.
**Cost:** medium; needs the ledger schema and the checks.
**Proof:** a field rename that breaks a v1 client is caught by the ledger, not by the model.

### 7.10 Convention drift, not style guide

**Attacks:** B (42% misalignment).
**Mechanism:** learn the *accepted* patterns for a component from merged PRs; flag only statistically unusual
drift. A convention file states the rule and the reason; the drift detector supplies the evidence.
**Cost:** medium; needs the pattern mining.
**Proof:** a change that follows the team's pattern is not flagged; a genuine drift is.

### 7.11 Deterministic evidence first, model second

**Attacks:** D, E (false positives, unverifiable claims).
**Mechanism:** run the repo's own checks (vet, staticcheck, oasdiff, semgrep, the tests) before the model; feed only
the diagnostics. The model's job is the semantic residue, not re-deriving what a tool already knows.
**Cost:** low; the sandbox exists.
**Proof:** the same diff reviewed with and without the diagnostics; precision rises, tokens fall.

### 7.12 Two-lane review with risk routing

**Attacks:** I (context budget), and the recall/precision trade-off.
**Mechanism:** lane 1 is a quiet co-reviewer (confirmed invariants + deterministic evidence only); lane 2 is a
thorough gate (high recall) on risky changes. Route by change size, component history, incident density, test
coverage, author. This matches the "Greptile vs Diamond" trade-off without running two vendors.
**Cost:** medium; needs a risk score.
**Proof:** on the bench, the quiet lane keeps precision on the clean PR and the deep lane keeps recall on the
buggy one.

### 7.13 Outcome-weighted retrieval and per-fact pruning

**Attacks:** I, and the "knowledge swamp".
**Mechanism:** a fact's rank is its posterior from real outcomes (cited → upheld, cited → dismissed, never cited).
A fact with no citation in 90 days is listed for pruning. This is the learning loop applied to retrieval, not just
to cards.
**Cost:** low; the event log exists.
**Proof:** a noisy fact falls below a proven one; a dead fact is pruned.

### 7.14 Human "why" capture on every decision

**Attacks:** B (misalignment).
**Mechanism:** every dismiss/uphold asks one question: "what would have made this useful?" The answer becomes a
card or a convention proposal. Cheap, high-signal, and it fixes the class that no model can infer.
**Cost:** low; the review flow already records the reason.
**Proof:** a dismissal reason becomes a card that stops the same comment next time.

---

## 8. Measurement: how we know it worked

The market's numbers are unstable. Ours must be honest.

### 8.1 The ablation bench

For each source S (graph, codebase facts, team facts, role rules, cards, POV, prior findings, heatmap), run N
reviews with S on and S off, same model, same skill, same diff. Score recall on the bugs S should catch, false
positives on a clean control, severity accuracy, tokens, latency.

```
knowledge gain(S) = Δ(recall + precision + severity accuracy) / (tokens added by S / 1000)
```

Report the table, including the sources that do not help. A source that never moves a decision is a pruning
candidate, not a success.

### 8.2 The test bench

For each test source (regression seeds, infra map, heatmap, oracle ladder, mutation gate), measure:

- **Oracle rate:** share of generated tests with a declared oracle ≥ rung 2.
- **Mutation score:** share of changed-line mutants killed.
- **Seed coverage:** share of the top regression seeds covered.
- **Flake rate:** share of generated tests that fail intermittently in 20 runs.
- **Survival:** share of generated tests still in the suite after 30 days.

A test that is deleted in a week was never a test.

### 8.3 The honesty rules

- One synthetic PR scored by its author is a mechanism demo, not a benchmark. Say so.
- A vendor number is a claim. Label the source and the methodology.
- "Not measured" is a valid result. Never write "should work".
- A held-out set is mandatory for any skill change. Train on one repo, validate on another.
- Publish the failure cases, not just the wins.

---

## 9. Phases

**P0.5 — the authoring path.** The CLI, the skill, `.heimdall/knowledge/`, the intake. (See
`CONTEXT_CAPTURE_PLAN.md` §11.) Without this there is no knowledge to scope.

**P1 — the VM catalog.** Postgres `knowledge_facts`, `fact_citations`, `context_packs`, `component_map`; the
cards volume; the compiler reading the catalog. No new service.

**P2 — the scopes.** `.heimdall/components.yaml`, `.heimdall/teams/*.md`, `.heimdall/roles/*.md`; the two
precedence orders; `read_context`; `read_pov`.

**P2.5 — review knowledge.** The six kinds, the incident fingerprint, the business-rule gate, the out-of-scope
gate, the compatibility ledger. Start with the kinds and the scope gate; the fingerprint and ledger are P3.

**P3 — test knowledge.** Regression seeds, the infra map, the heatmap, the oracle ladder, the mutation gate. This
is the half nobody else has; it is where the differentiation is.

**P4 — prove it.** The ablation bench and the test bench, held-out, with the failure cases. Put the numbers in
`REVIEW_MOAT.md`.

**P5 — the loops.** Outcome-weighted retrieval, per-fact pruning, human "why" capture, two-lane routing.

---

## 10. Gaps this document adds

| # | Gap | Fix | Done when |
|---|---|---|---|
| K1 | No Postgres knowledge catalog | the tables in 2.2 | a fact, its sources, its citations and its pack are queryable |
| K2 | No cards volume on the VM | mount `cards_data` mode 0700 | the compiler reads cards; no other container can |
| K3 | No scope dimension in retrieval | `scope ∈ {codebase, team, role, person}` + two precedence orders | a change in `deploy/**` gets team+infra facts, not refunds cards |
| K4 | No team knowledge | `.heimdall/teams/*.md` + Graphiti group `team:<team>` | a payments rule is visible to payments and not to the ledger |
| K5 | No business-rule gate | predicate layer + `kind: invariant` | a confirmed cap blocks without a model call |
| K6 | No out-of-scope gate | compare finding to criteria + intent | an out-of-scope nit never blocks |
| K7 | No incident fingerprint | diff embedding + nearest incident | a replay of a fix's parent retrieves the incident |
| K8 | No regression seeds | mine fix commits/reverts | a generated test fails on the parent and passes on the fix |
| K9 | No infra map | profile + `kind: procedure` | the test plan names services, fixtures, seam |
| K10 | No bug-class heatmap | derive from events + issues | the top 3 classes predict the next bug |
| K11 | No oracle gate | oracle rung + validator | a no-oracle test is refused |
| K12 | No mutation gate | changed-line mutants | a test that kills no mutant is refused |
| K13 | No compatibility ledger | version/client table + checks | a v1-breaking rename is caught deterministically |
| K14 | No per-fact cost/benefit | citations + pruning | a fact with no citation in 90 days is listed |
| K15 | No scope-aware severity | `out_of_scope` category | the gate distinguishes scope from severity |
| K16 | No two-lane routing | risk score + lane choice | the quiet lane keeps precision, the deep lane recall |

---

## 11. Open decisions

1. **Cards as files or Postgres rows?** I recommend files as the source of truth (private, inspectable) and a
   Postgres index for the compiler. The file is what the person owns.
2. **POV as a file or a table?** I recommend a file (`profile.md`) for the same reason; the index is optional.
3. **Team knowledge in `.heimdall/teams/` or in the policy?** I recommend `.heimdall/teams/` (context routing),
   keeping the policy as the merge gate.
4. **Incident fingerprint now or later?** Later (P3). The kinds and the scope gate come first; the fingerprint
   needs a real incident corpus.
5. **Mutation runner:** in the existing sandbox or a separate job? I recommend the sandbox, changed lines only.
6. **Compatibility ledger:** a new file or a section of `profile.yaml`? I recommend `.heimdall/compat.yaml`.
7. **Do we publish our bench?** I recommend yes, with the methodology and the failure cases. It is the
   differentiator against vendor-graded numbers.

---

## 12. Sources

Market and research, checked Nov 10, 2026:

- CodeRabbit field study, *Is Agentic Code Review Helpful?*, arXiv 2607.03316v2 (Jul 2026): 31,073 comments,
  239 repos; 36.4% accepted, 7.3% discussion, 56.3% rejected; 43.3% false positives, 10% out of scope, 4.7%
  redundant; 42% misaligned (23.7% intended trade-off, 18.3% preference).
- Cursor, *Bugbot now self-improves with learned rules* (Apr 8, 2026): reactions, replies, human comments;
  110k repos, 44k rules; vendor resolution table (Bugbot 78.13%).
- Cursor docs, Bugbot learned rules and `BUGBOT.md`; June 2026 update (3x faster, 22% cheaper, 10% more bugs);
  effort levels (Default 0.7 bugs/run, 79% resolution).
- CodeRabbit, *Code context: the evidence behind trustworthy AI code review* (Jun 24, 2026): code graph, 25+
  linters, knowledge base, Learnings, path instructions, Lost in the Middle, self-correction blind spot (64.5%
  average across 14 models).
- Martian Code Review Bench (Feb 2026 and Jul 30, 2026): CodeRabbit 51.2% F1 / 49.2% precision; Greptile 60.8% F1
  / 76.2% precision; benchmark instability across cuts.
- Entelligence 2026 benchmark: Codex 64.5%, Bugbot 39.4%, Greptile 36.9%, CodeRabbit 33.0% — rankings invert.
- Greptile content library, *Best AI Code Review Tools 2026*: Qodo governance-first, `.greptile/` config.
- Graphite Diamond: <5% negative-comment rate, 3.5% downvoted, 6% bug catch (July 2025 benchmark).
- 2026 study of 86,156 AI test changes (five agents): 80.2% weak or no explicit oracle signals.
- Diffblue next-gen platform: 80.7% coverage vs 32.3% developer + Claude Code; Meta TestGen-LLM 57%; Copilot 45%.
- Mutation-guided test generation: MutGen (arXiv 2506.02954), Meta ACH / LLMorpheus / μBERT (Sep 2025), ESEM
  2026 test-aware mutant generation.
- Property-based testing: Anthropic, *Finding bugs with Claude and property-based testing* (2026); 2025 OOPSLA
  result that a property-based test finds ~50x more mutants than an average unit test.
- Test oracle taxonomy: arXiv 2607.05031v3 (2026).
- Change impact analysis: ICSME 2019 semantic diff study (false positives are pernicious in review); *Rethinking
  Code Review in the Age of AI*, arXiv 2605.17548 (2026).
- CMU/Stanford, *AI Writes Faster Than Humans Can Review* (Jul 2026): PR volume 3.1x, reviewer pool 1.5x.
- Semgrep, *AI Code Review and the End of Reviewing Every Change* (Oct 2026): risk-tiered review.
- Cloudflare, *AI code review* failure classes: architecture, cross-system impact, concurrency.
