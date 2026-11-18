# SOTA quality hardening: mission brief for Opus/Fable (owner directive, Nov 18, 2026)

This is the quality bar after the D170 dev plan (H8 to H12) is finished. It is not a unit-test pass. The question is
whether Heimdall is genuinely excellent in the real world, good enough to sell, and whether its agents, lenses and
reviews are state of the art. Do not start by writing tests; start by writing down what "excellent" means, then find the
gap.

## Rule for the models doing this

Use the strongest available models (Opus/Fable) for the design, the desired-state documents, the architecture and logic
review, and the judgement of what is SOTA. Use DeepSeek for the high-volume agentic execution: it is cheap and can burn
tokens, so it runs the whole end-to-end and adversarial suite on every pass instead of a thin unit-test layer. No
laziness in the specs or the reasoning: if a claim is not backed by a real run, write `not verified`.

## Phase 0: write the desired state first (Opus/Fable, files before code)

Create one file per area under `docs/sota/`, each with two halves:

1. **Desired state.** The best-case result in concrete, observable terms: what a person sees, what the server records,
   what the agent does, what the timing and failure behaviour is. Derive it from `TECH_SPEC.md`, `MVP_SCENARIOS.md`,
   `HARNESS_DESIGN.md`, the product pitch and the owner's answers.
2. **What exists / what is missing.** A gap table with evidence: file and line, real run output, or `not verified`.
   No opinions without a pointer.

Areas (at least):

- `orchestration.md` — the Go server: rows, reconciler, effects, outbox, store, restart/recovery, concurrency,
  idempotency, timeouts, backpressure, observability, failure modes.
- `agent-reasoning.md` — the reasoning quality of every agent role: test author, reviewer, resolver, profiler, lead;
  the inputs they get, the tools they choose, the findings they file, the noise they avoid, the cases they handle.
- `agent-triggering.md` — every message and event that should start, resume, wake, reassign, time out or stop an agent;
  every platform and room; person vs agent senders; duplicate, replayed, stale and out-of-order events.
- `product-function.md` — the complete journey on the real systems, and every branch that can happen on the way.
- `lenses.md` — whether each lens and rule card is actually useful and SOTA: precision, recall, novelty, actionability,
  cost, overlap between lenses, and what a reviewer does with each result.
- `ux.md` — the person's experience: wording, timing, status, recovery, approvals, the board and every chat platform.
- `performance.md` — latency and cost per row, queue depth, token spend, runner time, database growth, leak risks.

## Phase 1: architecture and logic review (Opus/Fable)

- Read the desired state against the code and the event log. List architecture issues, logic issues and feature-mapping
  issues (a spec feature that is missing, half-wired, or wired to the wrong event).
- For each issue: severity, evidence, the smallest correct fix, the test that would have caught it, and whether it needs
  an owner decision.
- Ask the owner only when a decision or access change is genuinely needed; otherwise decide and record it.

## Phase 2: agentic end-to-end and adversarial testing (DeepSeek, designed by Opus/Fable)

Replace thin unit-test ceremony with an agentic harness that runs the real journey on the real systems on every pass:

- A ticket → room → branch → test task → red-first proof → commit → PR → checks → reviewer pick → three reviews →
  E1/E2/E3 verification → gate → human approval → merge.
- Ticketless PR, unknown ticket, two PRs for one ticket, push mid-review, force-push, closed PR, replayed webhook,
  duplicate command, restart mid-effect.
- Quiet agent, refusing provider, dead job, `heimdall resume`, busy runner, unreachable runner, invalid SARIF, timeout,
  the 5-call budget.
- Untrusted text in the ticket, PR, diff, check output and room post; prompt injection; person vs agent sender; spoofing.
- Cross-platform: Slack, Discord, Mattermost, GitHub and the web board; threads, edits, audiences, mentions.
- Review quality: planted in-diff and out-of-diff bugs, a card-only bug, a clean PR that must stay quiet, a reviewer
  overriding its own finding. Score precision and recall, not just pass/fail.
- Test competence: red-first, flake (3 runs), seam imports, placeholders, survivor answers, a test that passes without
  the feature.

The harness must capture, for every failure, the root cause with the event, tool call, prompt and output that caused it.
A failure without a root cause is not fixed. Record everything in `docs/sota/RUNS.md` with cost and `not verified` lines.

## Phase 2.5: performance and sellability hardening

- Measure every row: wall time, queue depth, token spend per task, runner time per job, database growth and sweep cost.
  Put the numbers in `docs/sota/performance.md` with the machine and the commit.
- Set budgets from the measurements (for example: a ticket reaches a branch and a test task inside a stated time; a
  review task costs no more than a stated token amount; a sweep over N open changes stays under a stated time). Fix the
  hot paths (event fold, reconciler sweep, claims, prompt size, redundant model calls) and re-measure. No micro-tuning
  without a before/after number.
- Make the demo sellable: one documented command or script brings up a fresh instance; a five-minute path shows ticket
  to merged PR with the real agents; screenshots and a short script are in the repo. If a step needs a manual repair,
  it is a defect, not demo preparation.

## Phase 3: quality bar and exit criteria

Heimdall is done only when all of these are true and evidenced:

- The full journey completes on the real AWS VM with real Jira, Switch, GitHub, OpenCode and the runner, without a manual
  repair, from a fresh ticket to a merged PR and a ticketless PR.
- Every agent starts on exactly the events it should, gets the right task and inputs, and stops for the right reason;
  no duplicate task, no lost wake, no stale head.
- The reviewers are SOTA: they find the planted and out-of-diff bugs, use the leads and checks, cite evidence that
  verifies, and file little or no noise on a clean PR.
- The lenses and rule cards are measurably useful; each has a precision/recall/actionability number and a reason to
  exist.
- The Go server survives restart, replay, concurrency and failure without duplicating a side effect or losing a change;
  latency and cost per row are recorded.
- The UX is polished on every platform: a person always knows what is happening, who is acting, and what to do next;
  no internal ids; findings are `F1`, `F2`.
- The adversarial suite runs again green after every fix, and the run artifacts and costs are in the repo.

## Guardrails (these do not relax for quality work)

- Real writes only on test objects: label `heimdall-test`, PR branches `heimdall-test/<run-id>`, product branches
  `<KEY>-*`; live PRs target `heimdall-demo`, never `main`. Approvals only from a login that is not the PR author.
- State the expected spend before a model run; the testing cap is $3 unless the owner raises it.
- Never read, print, log or commit `.env`, `secrets/`, tokens or private keys. VM agent `opencode.json` is off limits.
- Never weaken a check, lint rule, scenario or test to get green (AGENTS.md R2). Edits to guardrail files need the owner.
- A defect is fixed in code with a failing test first, then the whole agentic suite is re-run. Never patch the VM.
- Anything not run is written `not verified`, with the reason.
