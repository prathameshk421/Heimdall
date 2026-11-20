# Heimdall: product strategy (founder view)

Status: draft 1, Nov 21 2026, before the critic pass. Evidence: `research/01` to `research/05`, the code inventory in
section 3, and `docs/LIVE_RUN_H11.md`. Claims without a run say `not verified`.

## 1. The one sentence

**Heimdall is a self-hosted merge gate: nothing merges without proof, and the proof is on record.**

Proof means three things a person or an auditor can check without trusting a model:

1. A test, written from the ticket before the code, that failed and now passes.
2. Every blocking finding carries evidence: a failing test (E1), a tool result (E2) or a recorded team fact (E3).
   Reasoning alone (E4) never blocks and never reaches a person as a blocker.
3. A named person approved this exact commit. A new push voids the approval. The record is hash-chained.

Everything we build must make one of these three stronger, cheaper or easier to adopt. Anything else waits.

## 2. Why this wins (the market in five facts)

| Fact | Source |
|---|---|
| LLM reviewers comment; they do not gate. Copilot and Claude review cannot fail a check. Static tools gate but do not read intent. Nobody gates on "model finding with attached proof" | `research/01 §4, §6` |
| The top complaint about AI review is speculative noise. "I'd trust AI more if it brought proof instead of speculation" | `research/02 T1` |
| Review cannot keep up with AI PR volume (PR size up 154%, review time up 91%, vendor data) | `research/02 T2` |
| Change-control evidence is a paid requirement: SOC 2 CC8.1, ISO 27001 A.8.32, PCI DSS 6.2.3.1 and 6.5.1, DORA | `research/05 §3` |
| Self-host from CodeRabbit or Greptile needs an enterprise contract (CodeRabbit lists about $15k a month). The free self-host tools (PR-Agent, Kodus) have no gate | `research/01 §3`, `research/05 §2` |

**Positioning:** change assurance for teams that ship AI-written code. Sell the outcome (less review work, audit
evidence, code stays inside), not the mechanism ("deterministic gate"). Engineers get: "a PR only shows you what is
proven and what still needs your judgement." Security and compliance get: "an evidence record per merge, mapped to
your controls, and nothing leaves your network."

**First buyer:** the platform or security lead at a 30 to 500 developer company that already uses coding agents and has
an audit (SOC 2, PCI, DORA) or a rule against sending code to SaaS. Fintech, payments, EU finance, health tech, gov
suppliers.

**What we are not:** another comment bot, an auto-fixer, a coding agent, or a generic agent orchestrator.

## 3. Where the product really is (Nov 21, code inventory)

| Area | Fact | Pointer |
|---|---|---|
| Live journey | Tests flow worked live. Review, approval and merge did not finish (D6 output limit, D7 ticketless PR never reviewed) | `docs/HANDOFF.md`, `docs/LIVE_RUN_H11.md` |
| Switch coupling | Chat, agent runtime, identity and room membership are Switch-only; config requires `agent_runtime.provider == "switchroom"` | `internal/platform/config/config.go:551, 683` |
| Agents | 5 resident OpenCode seats, each a Unix user, managed by Switch's controller. No per-task jobs | `testdata/agents/agents.json`, `deploy/agents/` |
| Footprint | 6 compose containers + runner unit + controller + 5 to 6 agent units + firewall + an external Switch stack | `deploy/compose.yaml` |
| Model port | OpenAI-compatible HTTP already, but provider names are hardcoded to `deploy` and `openrouter` | `cmd/heimdall-server/model_provider.go:18` |
| VCS | GitHub App, polling, narrowed tokens. No merge queue, no GitLab | `internal/adapters/github` |
| Tracker | Jira only (plus a test file adapter). Ticket-first by design | `internal/adapters/tracker` |
| Sandbox | Rootless Podman, no network, strong flags. Only a Go profile ships | `internal/adapters/rootlessrunner/policy.go` |
| Install | About 15 commands across 9 steps, "an afternoon", needs Switch and Jira | `docs/SETUP.md`, `docs/COMPANY_SETUP.md:65` |
| Board | Read-only HTML + SSE, review report page. **No authentication** | `internal/transport/dashboard` |
| Cost | One partial live run: about $0.24 estimated, model calls not counted | `docs/LIVE_RUN_H11.md:89` |

The engine is good: event-sourced, a reconciler, an outbox, a pure-Go gate, a strong sandbox, narrowed GitHub tokens.
The shell around it is a hackathon demo. A buyer cannot install it without Switch, Jira and an afternoon, and cannot run
it on a non-Go repo.

## 4. The five strategic moves

1. **Finish the proof loop on real repos before anything else.** The live journey must reach a merged PR with no
   manual repair, and the reviewers must be measured on real open-source bugs (Proof Bench). This is the SOTA quality
   plan already under way; it is the floor, not a feature.
2. **Make Switch one adapter, not the platform.** The primary surface becomes the pull request itself (check run,
   proof summary comment, `/heimdall` commands) plus a native Slack app and a webhook notifier. Switch stays supported
   for teams that run it.
3. **Agents become short sandboxed jobs.** One task, one process, one container, then exit. Any CLI agent with a
   headless mode can run it (OpenCode, Claude Code, Codex, Goose, Aider). This deletes the controller, the resident
   seats and the firewall unit, and it lets idle cost go to zero.
4. **Install in ten minutes on any VM.** `heimdall init` plus one compose file with three services (server, Postgres,
   runner). Memory stack is an opt-in profile. Ticketless PR is the default path; Jira, Linear and GitHub Issues add
   context when present. Python, Node/TS, Java and Rust runner profiles ship in the box.
5. **Sell evidence to the people who pay for it.** Signed per-merge record, offline verifier, control-mapped audit
   pack, SSO on the board, shadow mode for trust before blocking. Then GitLab self-managed, the second-largest
   self-hosted host.

## 5. The cost claim, done honestly

"Cheaper than all alternatives" is a claim we must measure, not write.

Targets to measure on Proof Bench (not verified today):

| Metric | Target | Compare |
|---|---|---|
| Model cost per reviewed PR, median | under $0.30 | Bugbot about $1 to $1.50 per run; Claude review $15 to $25; CodeRabbit $24 to $48 per seat |
| Model cost per trivial PR (docs, lockfiles, under 20 lines) | $0 (deterministic skip) | everyone else charges a run |
| Server RAM idle | under 64 MiB | measured 42 MiB on Nov 7 |
| Whole Lite stack RAM idle | under 400 MiB | today about 1.5 GB+ with memory stack and resident agents, not verified |
| VM | 2 vCPU / 4 GB for a 50-dev team | today the reference is 2 vCPU / 16 GB |
| Time from install to first gated PR | under 15 minutes | today "an afternoon" |

How we get there: deterministic analyzers before any model call, a skip list for trivial changes, prompt prefix
caching by design, a hard per-PR token budget, flash models for triage and refute, a stronger model only for writing
proof, and agents that exist only while a task runs.

## 6. Pricing (proposal, to test with design partners)

| Tier | Price | For |
|---|---|---|
| Community | Free, source-available. Up to 10 active committers | Trials, small teams, OSS |
| Team | $15 per active committer per month, BYO model key | Most companies |
| Regulated | $35 per active committer per month, $20k a year floor | Audit pack, SSO, signed export, offline bundle, local models, SLA |
| Design partner | $5k for 90 days, credited to year one | First three logos |

The customer pays for the VM and the tokens. We print the token cost on every PR, so the bill is never a surprise.

## 7. What we refuse to build (for now)

- Auto-fix PRs and a coding agent of our own: crowded, and it breaks "independent of the writer".
- Our own LLM leaderboard from blog numbers.
- A Teams bot before a buyer asks.
- Bitbucket and Azure DevOps before a buyer asks.
- A hosted SaaS version before the self-hosted product has three paying users.
- Personal context packs as the headline. They stay as a later moat once the core is proven.

## 8. Risks

| Risk | Answer |
|---|---|
| GitHub or Anthropic adds a real gate to their reviewer | Self-host, audit record and exact-commit approval are where bundles do not go. Move fast on the audit pack |
| CodeRabbit and Qodo already use "governance" words | Show it: a tampered record fails the offline verifier on stage; a finding without proof is dropped live |
| Cheap models miss bugs | Measure on Proof Bench and say what we do not catch. Let buyers switch the model per role |
| The proof loop is slow (tests in a container per PR) | Cache toolchains, run only affected tests, budget the runner time and print it |
| We are a small team | One coherent loop, few integrations, done well. The roadmap is ordered by that |
