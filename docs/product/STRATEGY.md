# Heimdall: product strategy (draft 3)

Status: draft 3, Nov 21 2026. Draft 2 plus the changes in section 0, after critic 3 (`CRITIC-v2.md`) and the rival
check (`research/11`). Owner decisions are in section 9. Claims without a run say `not verified`.

## 0. Draft 3 changes (read first)

1. **A direct rival exists.** MergeHarbor (domain registered Oct 18 2026) documents proof runs, a merge gate with
   `proven_findings: block`, merge-base rule files, a regulated preset, self-hosting on Enterprise, 11 hosts, at $45 to
   $70 per developer per month. It has 0 GitHub stars, no customers, no funding and no launch we could find
   (`research/11`). Heal proves bugs with a test that fails twice and passes after the fix, without a gate. So the
   market shape is real and nobody owns it yet. Speed and distribution decide it.
2. **Our position is the open, model-free referee.** MergeHarbor's own verifier model confirms its findings and the
   gate uses a model confidence score. Heimdall puts no model in the gate, and it referees every reviewer, its own
   and any third-party bot's (MergeHarbor's included): proven findings block, unproven ones are not shown and are
   counted. The proof check is open source (MIT since Nov 27, 2026; O7) and runs
   in the customer's own CI with their own key. Closed rivals cannot follow us there.
3. **Honest words.** A test can prove a bug is real, never that a finding is false. "Dropped" becomes "unproven, not
   shown", and a person can open the list.
4. **Stricter proof.** A finding's test must fail twice on the PR head and pass on a tree where the suspected lines are
   reverted or fixed (Heal's rule). One red run is not proof.
5. **Money first.** Shadow mode stays free for open-source repos. For companies, the first sale is a paid pilot: $5k for
   30 days on one repo, ending with two printed numbers (proven findings per PR, p95 check time).
6. **Memory is trimmed, not cut.** The owner made it a priority. We keep what only we can do: approved rule cards that
   are signed, scoped at the merge base and enforced at merge with their history as proof. Session readers and
   redaction stay. The local MCP server and rule-file export wait.
7. **Jobs wait for a self-hosted partner, and they lose the independence claim.** A job PR is written by Heimdall, so
   it is reviewed by a different model and approved by a person, and we never call it independent.
8. **Claims removed.** "Rules from the merge base" is not unique (MergeHarbor does it). "Nobody ties blocking to
   proof" is false.

## 1. The one sentence

**Heimdall gives every AI-written change its proof before a person spends time on it.**

Proof is what a machine can check: a test that failed and now passes, a tool result, a recorded team rule. A finding
without proof is dropped and counted. A person then judges only what proof cannot settle, and approves the exact
commit.

Category: the proof layer for code that agents write. Not a review bot, not a coding agent, not a compliance tool.

## 2. What changed from draft 1, and why

| Draft 1 | Draft 2 | Evidence |
|---|---|---|
| First buyer: compliance lead at a regulated firm | First buyer: platform or DevEx lead in the middle of a coding-agent rollout | Both critics; no grassroots post ties AI review to SOC 2 (`research/06`); Vanta and Drata already own CC8.1 evidence (`CRITIC-deepseek §5`) |
| First surface: a VM install | First surface: the pull request, through a GitHub Action in shadow mode; the VM comes when the team wants jobs, memory sync or self-hosted git | Install friction; the real rival is a half-day DIY setup (`research/06`) |
| One blocking gate | A fast required check (deterministic, under 2 minutes) and a slow proof run that may say `unknown` and never blocks on a timeout | "A slow or flaky gate gets switched off" (`CRITIC-deepseek §6`) |
| Compete with AI reviewers | Ingest their findings and prove or drop each one | Teams keep the tool they like; incumbents cannot copy "we prove their comments" (`CRITIC-deepseek §3.6`) |
| Merge gate only | Three parts of one loop: the proof check, memory, proof jobs | `research/08`, `research/09` |
| Context packs later | Memory is the free open-source door | Demand is real (claude-mem 118k stars, Copilot Memory GA); remembering is not following (TellOnce: 57.5% of applicable preferences still broken) (`research/09`) |
| Audit pack as the wedge | Audit evidence as an upsell, exported into Vanta or Drata | `CRITIC-deepseek §5` |

## 3. The product: one loop, three parts

```
 developer laptop              pull request (any author: person, agent, bot)            company VM (optional)
 heimdall memory  ---rules--->  fast check  --->  proof run  --->  proof summary  <---  proof jobs
 (free, local)                  (required,        (async,         proven / dropped /    (upgrades, bug repro,
                                 < 2 min)          may be          needs you             missing tests)
                                                   unknown)              |
                                                                  person approves the exact commit
```

1. **The proof check, on every PR.** The product.
   - Fast check (required): secrets, analyzers (SARIF), pinned-test guard, diff coverage, approval bound to the head
     SHA, memory rules that apply to the changed paths. Deterministic, cached, under 2 minutes p95.
   - Proof run (async): red-first tests from the ticket or PR intent; every reviewer finding, ours or CodeRabbit's,
     Copilot's, Bugbot's, must get a failing test or a tool result, or it is dropped and counted.
   - One proof summary comment, updated in place: what is proven, what was dropped and why, what needs a person, what
     we did not check, and the cost.
   - Shadow mode first: two weeks of "would block because", then the team turns the check to required.
   - Break-glass: a bypass needs a reason and a named person, is recorded, and shows as open until reviewed.
2. **Memory, the free door.** `heimdall memory` is an open-source local CLI. It reads the developer's own agent
   sessions (Claude Code, Codex, OpenCode, Cursor), redacts secrets on the laptop, and turns repeated corrections into
   rule cards the developer approves. Every agent the developer uses gets them (local MCP server, generated
   AGENTS.md / CLAUDE.md / Cursor rules). With a Heimdall server, approved cards are signed, scoped to code their owner
   approves at the merge base, and checked on every PR as E3 evidence. Hard rule: no per-person scores for managers.
3. **Proof jobs, on the company VM.** The same sandbox and the same gate do work whose result a machine can check,
   and hand it over only with proof. First job: dependency and CVE upgrade PRs that arrive green (fix the red
   Renovate/Dependabot PR, prove tests pass and the vulnerability is gone, test files unchanged). Next: bug report to
   failing repro test to fix; tests for changed code that must catch planted mutants.

Why the three are one product: memory supplies rules, jobs supply changes, the proof check is the one place both are
judged. Each part makes the others stronger. Nothing here is a separate app.

## 4. Who buys, and why now

| Buyer | Trigger | What they get | Proof we must show |
|---|---|---|---|
| Platform / DevEx lead (first) | Rolling out coding agents; review queue grows; agent PRs need a policy | Agent PRs arrive with proof or get dropped; one page per PR | Drop count and p95 time on their own repo in shadow mode |
| Engineering manager | AI review noise; seniors stuck reviewing | Comments cut to the proven ones | "40 comments in, 4 proven out" on their repo |
| Developer (free door) | "I told the agent that already" | Their rules follow them to every agent | Fewer repeated corrections, measured locally |
| Security / platform at self-hosted git shops (VM) | Code cannot leave; GitHub Enterprise Server, GitLab self-managed | Proof check plus upgrade jobs on their VM | Upgrade PRs merged with proof, CVE count down |
| Compliance (upsell, after 90 days) | Audit | Evidence per change exported to Vanta/Drata, break-glass records | A sample export an auditor accepts (not verified) |

## 5. Why we win

- **Proof as the unit.** LLM reviewers comment; static tools gate without intent. Nobody drops unproven findings and
  prints the count (`research/01 §6`). Cheap models are good enough when a machine checks the result
  (`research/06`: a DeepSeek-flash pipeline matched CodeRabbit on a 50-PR bench for about $7, not verified by us).
- **Independent of the writer.** Cursor, Devin and Factory review their own output. We are the second opinion with
  evidence, next to whatever agent the team picked.
- **No model in the gate, and every reviewer refereed.** Rivals let a verifier model or a confidence score decide.
  We let only proof decide, and we referee other bots' findings too (`research/11`).
- **Open.** The proof check is source-available and runs in the customer's CI. MergeHarbor, CodeRabbit and Greptile
  are closed.
- **Runs where the code lives.** The customer's CI runner or their own VM. Self-hosted review from rivals needs an
  enterprise contract (`research/01 §3`).
- **Memory that is enforced.** Memory tools remember. We check, at merge, with the card's history as proof.

What is not a moat: mining sessions (five open tools do it), approval on the head SHA (a GitHub setting), the word
"governance" (CodeRabbit and Qodo use it).

## 6. Where the product really is (Nov 21)

The engine is solid: event-sourced Go server, reconciler, outbox, pure-Go gate, rootless no-network sandbox, narrowed
GitHub App tokens, card ranker and learning loop. The shell is a hackathon demo: Switch is the only chat and agent
runtime (`internal/platform/config/config.go:551`), agents are 5 resident seats, only a Go runner profile ships, the
board has no auth, install takes an afternoon, and the live run (H11) reached tests but not a merge (`docs/archive/hackathon/HANDOFF.md`).
Full inventory: `research/` notes and draft 1 section 3.

## 7. Lightweight by design (claims to measure, not to write)

| Metric | Target | Today |
|---|---|---|
| Fast check p95 | under 2 minutes on a 10-repo bench | not measured |
| Proof run p95 | under 15 minutes, `unknown` after the cap | not measured |
| Model cost per reviewed PR (median) | under $0.30, $0 on trivial PRs | one partial run, about $0.24, not verified |
| Action mode footprint | one static binary in the customer's runner, no database | does not exist |
| Server Lite stack RAM idle | under 400 MiB, 2 vCPU / 4 GB VM for 50 developers | server 42 MiB, whole stack not measured |
| Install to first proof summary | Action under 10 minutes; VM under 30 minutes | an afternoon |

How: deterministic checks before any model call; skip models on trivial changes; prompt prefix caching; a hard
per-PR token budget; flash models for triage and refute, a stronger model only to write proof; agents that exist only
while a task runs; the customer's runner already has the toolchain in Action mode.

## 8. Pricing (proposal, to test with design partners)

| Plan | Price | Includes |
|---|---|---|
| Free | $0 | `heimdall memory`; the source-available proof check on open-source repos, BYO model key |
| Pilot | $5k for 30 days, one repo | Hands-on setup; ends with proven findings per PR and p95 check time on their repo |
| Team | $15 per active committer per month | Required check, break-glass, memory sync and enforcement, proof summary history |
| Jobs | 50 merged proof PRs a month included per VM; then $6 per simple upgrade, $30 per breaking-change fix; failed, unmerged or reverted-in-7-days work is free | Upgrade, repro and test jobs on the company VM |
| Regulated | $35 per active committer per month, $20k a year floor | Self-hosted git adapters, signed record and verifier, Vanta/Drata export, retention and legal hold, SSO, SLA |

Every PR prints its model cost. A monthly cap per repo is a setting.

## 9. Owner decisions (I made a call; overrule any)

| # | Decision | My call |
|---|---|---|
| O1 | Change the first buyer from compliance to the platform lead in an agent rollout | yes |
| O2 | Lead with a GitHub Action in shadow mode; the VM is the upgrade | yes |
| O3 | Open-source `heimdall memory` (licence: Apache-2.0 for the CLI, server source-available) | superseded by O7 on Nov 27: the whole repository is MIT-2.0 |
| O4 | No per-person score for managers, ever | yes |
| O5 | Rename `cards` to `memory` in the CLI | yes |
| O6 | Switch becomes one optional adapter | yes |
| O7 | ~~Source-available proof check under FSL (Apache-2.0 after two years)~~. Changed Nov 27, 2026 by the owner: the whole repository, proof check included, is open source under MIT (`LICENSE`; Apache-2.0 for a few hours first) | yes |
| O8 | First company sale is a $5k 30-day paid pilot, not free shadow mode | yes |

## 10. What we refuse to build (for now)

Auto-fix of reviewer findings and our own coding agent; flaky-test fixing as a product (Trunk retreated,
`research/08`); a sandbox-as-a-service business; canary analysis and rollback (Argo Rollouts, Harness, LaunchDarkly own
it; `research/09 §6`); a Teams bot, Bitbucket or Azure DevOps before a buyer asks; a hosted SaaS before three paying
users; a leaderboard from blog numbers.

Deployment work that does fit: release evidence (what shipped, with which proof), and agent-run smoke tests against
staging as one more proof source (`research/09 §6`).

## 11. Risks

| Risk | Answer |
|---|---|
| The check is slow or flaky and gets bypassed | Fast/slow split, `unknown` never passes silently, p95 published, break-glass recorded |
| Generated tests from tickets are weak (23.6% to 37% in benches, `CRITIC-deepseek §7`) | Red-first bench before claims; test integrity checks; never count a weak test as proof |
| Incumbents ship "agent fixes until green" | Proof and independence, self-hosted git, merge-base rules |
| Memory feels like surveillance | Local-first, developer approves every card, no manager scores, works-council friendly |
| Small team, wide surface | One loop. Build order follows the loop. Cut anything that does not feed it |
