# 01. Competitive landscape: AI code review and merge gates (as of 2026-11-20)

Scope: tools that review AI-written or human-written pull requests. Heimdall is the reference point: a self-hosted Go gate where no model decides, findings need evidence (E1 failing test, E2 deterministic tool result, E3 team-memory fact), and a human approves the exact commit.

Method and honesty rules:
- Research date is 2026-11-20. Prices change often. Almost every vendor moved from seats to usage billing in 2026.
- Vendor pages were read directly only where a [P] tag appears. All other numbers come from aggregators or competitor blogs and are tagged [S] (secondary). Treat [S] numbers as "not verified" against the vendor page.
- Benchmarks are mostly vendor-run. Vendor-run numbers are marked "vendor-run".
- Where a source could not be found, the cell says "not verified".

Source tags: [P] = vendor primary page read in this research. [S] = secondary (blog, aggregator, press). Numbered sources are in the Sources list at the end.

---

## 1. Big comparison table

Columns: Deploy = SaaS / self-host / BYO-LLM / air-gap. Hosts = GitHub (GH), GitLab (GL), Bitbucket (BB), Azure DevOps (AZ), Gitea/Forgejo (GT). Gate = can it block a merge on its own. Tests = writes or runs tests.

| Tool | Deploy | Price (date) | Hosts | Gate | Tests | Benchmark (who ran it) | Funding / traction |
|---|---|---|---|---|---|---|---|
| CodeRabbit | SaaS; self-host for Enterprise only; BYO LLM listed in docs [P1,S12] | Essentials $24 yr / $30 mo; Team $48 / $60; Advanced $72 / $90 per dev per month; over-limit $0.25 per file (checked 2026-11) [P1,S5] | GH, GL, AZ, BB (self-host: GHES, GL self-managed, AZ, BB DC); GT not found [P1,S12] | Conditional: needs "error mode" and a Request Changes flow; default is warn [S9] | Unit test generation on Team tier [P1] | Martian Code Review Bench snapshots: 51.2% F1 (early 2026), 60.5% F1 (later snapshot) [S6]. Greptile-run 2025: 44% catch [S6] | $143M Series C at $1.5B, 2026-08-12; $60M Series B 2025-09 at $550M [S7] |
| Greptile | SaaS; self-host Enterprise only [P2] | Pro $1 per credit, no seat fee (base review 1 credit, Plus 3, Apex 10); Starter free, 50 credits/mo; third parties still list $30 per seat [P2,S5] | Enterprise: GHES, GHEC, GL self-managed, BB DC, Gitea, Perforce [P2]. Starter/Pro hosts not listed | Posts a status check; fail rule not documented [S9] | Runtime validation runs generated tests in a sandbox (vendor-adjacent claim) [S8] | Vendor-run 2025: 82% catch, vs CodeRabbit 44%, Bugbot 58%, Copilot 54% (50 PRs) [S6]. Martian: 64.5% F1, 83.6% precision, 52.5% recall in one snapshot [S6] | $25M Series A 2025-09, Benchmark, reported $180M valuation [S10] |
| Graphite (Cursor) | SaaS | Hobby free; Starter $20 yr / $25 mo; Team $40 / $50 per user per month; Enterprise custom (as of 2026-10-04) [S5] | GH only (not verified for others) | Merge queue (stack-aware); AI review is advisory | No | none found | Cursor agreed to buy it 2025-12-19; price not disclosed; last private valuation about $290M [S3] |
| Cursor Bugbot | SaaS | Usage billing since 2026-05-11: about $1.00 to $1.50 per run. Old price $40 per seat per month. Annual plans switch at renewal [P3] | GH, GL (incl. self-hosted), BB Cloud and DC, AZ Services (beta; AZ Server not supported) [S11] | Conditional: needs required check plus fail-on-unresolved; neutral on error [S9] | No | Cursor claim: 80% resolution rate; high effort finds 35% more bugs [P3]. Augment-run: 60% precision, 41% recall (unconfirmed by Martian) [S6] | Part of Cursor |
| GitHub Copilot code review | SaaS (GitHub-hosted); no BYO | Since 2026-06-01: AI credits plus Actions minutes. Plans: Pro $10, Pro+ $39, Max $100, Business $19, Enterprise $39. About $0.05 to $1 per review (Lite), $0.25 to $5 (Balanced) [S5,S4] | GH only [S9] | No native block; comments only. An opt-in approving review in public preview 2026-09-01 (not verified) [S9] | No | Martian: 59.5% F1 in one snapshot; 43.5% F1 in an earlier one (643K+ PRs) [S6] | Microsoft |
| Qodo (Merge, PR-Agent, Cover) | SaaS; self-host and air-gap for Enterprise; BYO LLM | June 2026 move to credits: $0.012 per credit; Pro Team listed at $30 (unit unclear) with 5K / 20K credit packs [S4,S5] | GH, GL, BB, AZ (PR-Agent also GT) [P4] | Depends on setup [S9] | Qodo Cover (test generation): status not verified | Qodo claim: 50.3% F1 on Martian bench; 63.4% precision (vendor-reported) [S6,S13] | $70M Series B 2026-03-30, total $120M, Qumra lead [S13] |
| PR-Agent (open source) | Self-host: CLI, Action, Docker, webhook. Any LLM via LiteLLM incl. Ollama [P4] | Free (MIT) | GH, GL, BB, AZ, GT [P4] | Comments only; gating is DIY via CI | No | none | 16.5k stars; Qodo gave it to the community in April 2026; v0.47.0 on 2026-10-02 [P4,S14] |
| Ellipsis | SaaS | Own blog, April 2026: $0.25 per review. Trackers list $20 per user per month [S15] | GH (GL not verified) | No | Can write fixes (six agent types) [S15] | none found | not verified |
| Bito | SaaS; self-host not verified | Team $12/$15; Professional $20/$25; 5,000 lines per seat then $5 per 1,000 lines (as of 2026-10-04) [S5] | not verified | No | No | none found | not verified |
| Korbit | Unclear. Reported bought by Boost Security 2026-05-06; sign-up hosts reportedly dead [S16] | n/a | n/a | n/a | n/a | n/a | treat as gone |
| Sourcery | SaaS | Pro $12 / $15; Team $24 / $30; free on public repos (2026-10-04) [S5] | GH, GL (not verified further) | No | No | none found | not verified |
| Baz | SaaS | Pro about $30 per active dev per month (aggregator) [S5b] | GH (not verified) | not verified | No | none found | $17M seed total after $9M extension, 2026-06-29 [S17] |
| cubic | SaaS | Team $30 / $40; Pro $79 / $99; Max $160 / $200 per dev per month; Starter free (2026-10-04, vendor blog) [S5] | GH | No | No | none found | YC 2025; seed about $500K per one source; a $68M claim is not verified [S18] |
| Macroscope | SaaS | Usage: about $0.05 per KB reviewed, about $0.95 per review average; $100 free credit (aggregator) [S5b] | GH | No | No | none found | $30M Series A (Lightspeed) announced 2025-09-17; about $40M total [S19] |
| Gitar | SaaS; Sonar now owns the line | Per-user plans on AWS Marketplace; Sonar pages say per-PR pricing. Conflict, not verified [S20] | GH (CI: CircleCI, Buildkite, Bitrise for Pro) | Fixes CI failures and can commit; not a gate | Reruns CI until green | none found | $9M seed 2026-04-15 (Venrock); now sold under Sonar [S20] |
| Claude Code Review (managed) | SaaS on Anthropic infra; no self-host; not for ZDR or HIPAA orgs | $15 to $25 average per review, Team or Enterprise plan required (secondary, 2026) [P5,S5] | GH incl. GHES and GHEC [P5] | No. Check run never concludes as failure [P5] | Does not build or run code [P5] | none independent. Qodo claims it beats Claude on Martian F1 [S13] | Anthropic |
| Claude Code GitHub Action / GitLab CI | Runs in your CI; BYO API key or cloud credentials | No per-review fee: your tokens and CI minutes [P5,S5] | GH, GL [P5] | Only via your own CI logic | Whatever your CI runs | none | Anthropic |
| OpenAI Codex review | SaaS; `@codex review` on GitHub | Not in the cheapest plan; credit-based on Business and Enterprise. One user reported 25 credits per PR (not verified) [S21] | GH | Never blocks on its own; flags P0 and P1 only [S21,S9] | No | none found | OpenAI |
| Gemini Code Assist review | SaaS | Enterprise about $45 per user per month annual (secondary). Free GitHub bot reported ended 2026-07-17 (secondary, not verified) [S22] | GH | No native gate [S9] | No | none found | Google |
| Google Jules | SaaS coding agent, not a reviewer | Free: 15 tasks per rolling 24 h (secondary) [S22] | GH | n/a | Writes code and tests | none found | Google |
| Amazon Q / CodeGuru | AWS SaaS | CodeGuru Reviewer closed to new repos 2025-11-07; CodeGuru Security ended 2025-11-20. Q Developer IDE and paid plans end 2027-04-30; move to Kiro (secondary) [S23] | CodeCommit, GH, BB, GL via CodeGuru (not verified) | No | No | none | AWS |
| Sonar (SonarQube, AI Code Assurance, Gitar) | SaaS and self-host (SonarQube Server); air-gap possible | Team plan shown at $34 per month on Sonar pricing page; Enterprise custom; sources conflict [S24] | GH, GL, BB, AZ [S24] | Yes. Quality Gate is a required check [S24,S9] | No (static analysis; coverage gate) | n/a | Large incumbent; bought Gitar |
| Snyk (DeepCode AI) | SaaS; self-host limited | Team about $25 per dev per month (secondary) [S25] | GH, GL, BB, AZ | Yes via required check [S9] | No | n/a | Large incumbent |
| Semgrep (Assistant) | SaaS; OSS engine free (LGPL, not verified) | Assistant listed $20 or $35 per contributor per month on staging pages: not verified [S25] | GH, GL, BB, AZ | Yes: block mode returns a failing exit [S9] | No | n/a | Established |
| Aikido | SaaS | Bundle pricing; $20 to $24 per user per month per module (competitor blog, not verified) [S26] | GH, GL, BB, AZ | Yes via CI gate | No | n/a | Unicorn-stage (not verified) |
| CodeAnt AI | SaaS; self-host Enterprise (not verified) | $10 to $24 per user per month across sources; not verified [S26] | GH, GL, BB, AZ | Quality gates (vendor claim) | No | n/a | not verified |
| Panto | SaaS | $12 to $15 per dev per month; free for open source [S26] | GH, GL, BB, AZ (not verified) | No | No | n/a | not verified |
| Kodus (open source) | Self-host (AGPLv3, `ee/` commercial) or cloud; BYO key; Ollama and vLLM via OpenAI-compatible URL [S27,S28] | Community free; Teams $10 / $8 per dev plus tokens (repo claim, not verified on vendor page); tokens at provider list price [S27] | GH, GHES, GL self-managed, BB DC, AZ, Forgejo [S28] | not stated | No | none | selfhosted-2.2.4 on 2026-09-27 [S28] |
| Tabnine review | SaaS, self-host and air-gapped (Enterprise) | Enterprise about $39 per user per month (secondary) [S25] | GH, GL, BB (not verified) | No | No | none | Established |
| JetBrains (Qodana, Junie) | Qodana self-host / SaaS | not researched | not verified | Qodana quality gates exist (not verified) | No | none | n/a |
| Augment Code Review | SaaS | About 2,400 credits (about $1.50) per PR (launch post); plan tiers in flux [S29] | GH | No | No | Vendor-run: 59% F-score, about 65% precision, 57% recall [S29] | Well funded (not verified) |
| Devin Review | SaaS | Free preview ended; 2-week trial then usage billing (2026-04-14). Reported $0.25 to $0.75 per PR (secondary) [S30] | GH | No | Devin can write tests; Review itself does not run them (not verified) | none independent | Cognition |
| Factory (Droid review) | SaaS, hybrid, on-prem, air-gapped (tear sheet, not verified) | Not published. Tier prices unverified [S31] | GH (not verified) | No | Droids can run QA workflows | none found | $200M round 2026-09-15 at $5B (secondary) [S31] |
| GitLab Duo Code Review Flow | GitLab.com, Dedicated, self-managed 18.8.4+ | Flat $0.25 per review (0.25 GitLab Credit), 2026-03-19 [S32] | GL only | Comments; GitLab approval rules apply | No | none | GitLab |
| Mira, Gito, ai-code-review, Open Code Review, Shippie | Self-host OSS | Free | GH, GL, some GT/Forgejo, BB | Not stated | No | none independent | small projects [S28] |

---

## 2. Per-tool notes

CodeRabbit. Category leader by traction: 20,000 customers and 400,000+ monthly users claimed [S7]. Hourly rate limits per developer: Essentials 5 PR reviews per hour, Team 8, Advanced 10, Enterprise 12 [P1]. Self-host is Enterprise only, and the GitLab self-host page says 500+ seats [S12]. Weakness: noisy comments per many user reports (anecdotal, not verified). Blocking is opt-in.

Greptile. Highest precision on one Martian snapshot (83.6%) but the lowest recall of the leaders [S6]. Self-host only on Enterprise, with the widest self-hosted git list (GHES, GL, BB DC, Gitea, Perforce) [P2]. The only tool in this set that claims runtime test validation in a sandbox [S8]. Pricing flipped from seats to credits in 2026, and aggregators disagree.

Graphite / Cursor. Cursor bought it to own review [S3]. Strength: stacked PRs and a stack-aware merge queue. Risk: the owner also sells the coding agent, so the reviewer is not independent of the writer. Graphite is still sold as its own product.

Cursor Bugbot. Widest host list among SaaS tools (GH, GL, BB Cloud and DC, AZ Services) [S11]. A review costs about $1.00 to $1.50 and runs per push, so cost scales with commits, not PRs [P3]. Gate only if you wire required checks.

GitHub Copilot review. Biggest install base (643K+ PRs in the Martian sample) but weaker F1 [S6]. From 2026-06-01 it uses Actions minutes plus AI credits, so spend is hard to predict [S4]. Comments only.

Qodo. Pivoted to "code verification and governance" with a $70M raise [S13]. Cross-repo review, mined rules and skill governance are in its release notes (not verified). PR-Agent is the open core. Qodo moved it to community ownership in April 2026, a signal that the vendor does not put weight on the self-host path [P4].

PR-Agent. MIT, five git hosts, any LiteLLM model [P4]. This is the closest free base for a self-hosted reviewer. It has no gate, no evidence rules and no human-approval record.

Ellipsis, Bito, Sourcery, Baz, cubic, Macroscope, Panto, CodeAnt. Small SaaS reviewers at $12 to $40 per seat or $0.25 to $1.50 per review. None documents a native merge gate. Baz is moving up to planning-stage review (Baz Planner, review against an architecture spec before code exists) [S17]. Macroscope charges by size (KB), which punishes large diffs [S5b].

Korbit. Reported acquired by Boost Security in May 2026 and hard to reach. Do not count it as a live competitor [S16].

Gitar / Sonar. Different job: it fixes failed CI and re-runs until green. Now inside Sonar, which already owns the deterministic gate side [S20,S24].

Claude Code Review. Multi-agent with a verify step, GitHub only, on Anthropic infrastructure, read-only, never builds or runs code, and the check run never fails [P5]. That last point is a clear product choice: the decision stays with humans, but there is no machine gate.

Codex review. `@codex review` posts P0 and P1 issues only [S21]. No block by itself.

Gemini Code Assist, Jules, Amazon Q, CodeGuru. Google and AWS are retreating or folding products into Antigravity and Kiro (secondary, not verified) [S22,S23]. Low risk for Heimdall.

Sonar, Snyk, Semgrep, Aikido. These are the real merge gates today. Their output is deterministic (rules, SAST, quality gates) and fits Heimdall's E2 evidence class. They are inputs to Heimdall, not rivals [S9,S24].

Devin Review, Augment, Factory. Coding-agent vendors adding review. Factory advertises air-gapped deployment and a $5B valuation (secondary). Devin and Augment publish usage-based per-PR prices of about $0.25 to $1.50 [S29,S30,S31].

GitLab Duo. $0.25 per review flat, works on self-managed GitLab [S32]. A cheap native option for GitLab shops, but advisory.

---

## 3. Open source and self-hostable with local LLMs

| Project | License | Hosts | Local LLM | Last release seen | Note |
|---|---|---|---|---|---|
| PR-Agent | MIT | GH, GL, BB, AZ, GT | Ollama via LiteLLM [P4] | v0.47.0, 2026-10-02 [S28] | Community-owned since April 2026 |
| Kodus | AGPLv3 + commercial `ee/` | GH, GHES, GL, BB DC, AZ, Forgejo | Any OpenAI-compatible URL (vLLM, Ollama, TGI, LiteLLM) [S27,S28] | 2026-09-27 | Most complete OSS product; AGPL matters if you embed it |
| Gito | MIT | GH, GL (beta) | Ollama, vLLM, llama.cpp, SGLang, LM Studio | v4.5.0, 2026-09-05 | |
| ai-code-review | MIT | GL, GH, Forgejo | Ollama | 2.7.3, 2026-09-03 | 21 GitLab stars; tiny |
| Open Code Review | Apache-2.0 | GH, GL | Needs native tool calling | v1.12.12, 2026-10-05 | |
| Shippie | MIT | GH, GL | Not listed | v0.21.2, 2026-06-23 | |
| Mira | not verified | GH, GL, Forgejo, BB Cloud | not verified | not verified | Vendor benchmark only |

Commercial self-host: CodeRabbit Enterprise (GHES, GL, AZ, BB DC; 500+ seat note on GitLab) [S12]; Greptile Enterprise [P2]; Qodo Enterprise; Sonar; Tabnine (air-gapped) [S25]; Proval (commercial, local models) [S27b]. All require a sales contact. No public price.

Cheapest viable self-host (as of 2026-11-20):
1. PR-Agent in a Docker container or webhook, with Ollama or vLLM, on an existing VM: $0 licence. Cost is the model. A 2 vCPU / 16 GB box with no GPU can run only small CPU models, so reviews will be slow and weak. A hosted cheap model (for example DeepSeek through OpenRouter) is the real "cheap" path. Token cost per review: not verified.
2. Kodus Community: $0 licence, AGPL, adds a UI and more hosts.
3. Claude Code Action in your own CI: no licence, you pay tokens and CI minutes [P5].
Quality caveat: one 2026 guide says self-hosting with Ollama or vLLM pays off only with GPU infrastructure and strict data rules [S28].

---

## 4. Gating reality (key finding)

Source for this section is a Codacy blog (competitor, 2026-10-26), so check vendor docs [S9]:
- Cannot block natively: GitHub Copilot review, Claude Code Review, Gemini Code Assist. The Claude page confirms the check run never concludes as failure [P5].
- Conditional: CodeRabbit (error mode), Bugbot (required check plus fail-on-unresolved), Codex (wire the Action), Qodo (setup dependent).
- Real gates: SonarQube, Semgrep, Snyk, Codacy, DeepSource, Qlty. All are deterministic.
- Nobody in this set blocks on "model finding with attached proof". LLM reviewers comment. Static tools block. They are two separate worlds.

---

## 5. Benchmarks summary

| Benchmark | Who | Result | Caveat |
|---|---|---|---|
| Martian Code Review Bench, early-2026 snapshot (about 300K PRs) | Martian (independent; uses "did the developer change the code" as the label) | CodeRabbit 51.2% F1 (49.2% P, 53.5% R); Copilot 43.5% F1 [S6] | Snapshots shift; live page returned no data to the fetch tool |
| Martian, later snapshot | Martian | Greptile 64.5% F1 (83.6% P, 52.5% R); CodeRabbit 60.5%; Copilot 59.5% [S6] | Secondary summary |
| Greptile July 2025 | Greptile (vendor-run) | Catch rate: Greptile 82%, Bugbot 58%, Copilot 54%, CodeRabbit 44% (50 bug-fix PRs, 5 repos) [S6] | Small, vendor-run |
| Augment launch | Augment (vendor-run) | Augment 59% F, 65% P, 57% R; Bugbot 60% P / 41% R; Copilot 20% P / 34% R [S6,S29] | Martian has not confirmed |
| Qodo | Qodo (vendor-run) | 50.3% F1 on Martian bench [S13] | Disagrees with other snapshots |
| Tenki 2026 | Tenki (competitor) | Greptile 36.1%, CodeRabbit 28.7% [S6] | Different method |
| Cursor | Cursor (vendor-run) | Bugbot 80% resolution rate [P3] | Resolution is not recall |

Take-away: no stable independent ranking exists. F1 in the 45 to 65% range means about half of real issues are missed or half of comments are noise. No benchmark measures "finding backed by a reproducing test", which is Heimdall's unit of output.

---

## 6. White space Heimdall can own

1. Evidence-required findings. No tool in this set refuses to post a blocking finding without a failing test (E1), a tool result (E2) or a recorded team fact (E3). The closest are Greptile runtime validation and Ito/Tembo (sandbox test runs), but none ties blocking to proof [S8, search results].
2. A gate no model controls. LLM reviewers cannot block (Copilot, Claude) or block only with extra setup (CodeRabbit, Bugbot). Deterministic tools block but do not read intent. A plain-Go gate fed by evidence-typed findings sits between the two.
3. Human approval bound to the exact commit. Competitors review per push but none sells "approval of this SHA, invalidated on any new push, with an audit record". CodeRabbit itself says a blocking check needs a human override and audit trail [S9].
4. Test-first on the write side. Ticket to failing tests to PR is a full loop. Review tools start after the PR exists. Planning-stage review (Baz Planner) is the only move toward the left side [S17].
5. Independent, multi-context reviewers. Cursor now owns Graphite, and coding-agent vendors (Devin, Augment, Factory) sell review of their own output. Heimdall's reviewers each carry one developer's context and are separate from the writer.
6. Cheap and self-hosted on one VM. Enterprise self-host from CodeRabbit and Greptile needs a sales call (and 500+ seats at CodeRabbit on GitLab). PR-Agent has no gate. Kodus is AGPL. A 42 MiB Go binary with no-network rootless Podman tests is a different class of footprint.
7. Chat as the control room. Switch rooms bridged to Slack, Discord, Mattermost and Telegram. No researched competitor offers merge-gate decisions in chat across four platforms. Not verified exhaustively.
8. Predictable cost. Usage billing at Copilot, Bugbot ($1 to $1.50 per run) and Claude ($15 to $25 per review) is hard to forecast. Cheap models (DeepSeek via OpenRouter) plus evidence rules cut calls that never block anything. Per-review cost for Heimdall: not verified, measure it.

Risks and gaps to watch:
- Claude Code Review and Copilot are free of friction for GitHub teams and may add a gate. Today they do not [P5,S9].
- Martian-style benchmarks reward comments developers act on, not proof. Heimdall should publish its own bench: findings with E1/E2/E3 evidence, precision and cost per merged PR.
- Gitea and Forgejo are weakly covered by everyone except PR-Agent, Kodus and Greptile Enterprise. This is a niche to take if Heimdall supports it.
- Sonar owning Gitar plus a deterministic gate means the incumbent could add LLM review with a gate quickly.

---

## Sources

[P1] CodeRabbit plans and limits, read 2026-11-20: https://docs.coderabbit.ai/management/plans.md
[P2] Greptile pricing, read 2026-11-20: https://www.greptile.com/pricing.md
[P3] Cursor, May 2026 Bugbot changes (post dated 2026-05-11): https://cursor.com/blog/may-2026-bugbot-changes
[P4] PR-Agent repository, read 2026-11-20: https://github.com/qodo-ai/pr-agent
[P5] Claude Code Review docs, read 2026-11-20: https://code.claude.com/docs/en/code-review

[S3] Cursor acquires Graphite (2025-12-19): https://cursor.com/blog/graphite ; https://www.axios.com/pro/enterprise-software-deals/2025/12/19/cursor-buys-code-review-platform-graphite ; https://www.techzine.eu/news/devops/137473/cursor-strengthens-ai-coding-platform-with-acquisition-of-graphite/
[S4] GitHub Copilot usage billing and Actions minutes (2026-04-27): https://github.blog/changelog/2026-04-27-github-copilot-code-review-will-start-consuming-github-actions-minutes-on-june-1-2026/ ; https://github.blog/news-insights/company-news/github-copilot-is-moving-to-usage-based-billing/ ; Qodo pricing summaries: https://usagepricing.com/blueprint/qodo ; https://pricingsaas.com/companies/qodo
[S5] cubic pricing comparison (vendor blog, rows verified 2026-10-04): https://www.cubic.dev/blog/ai-code-review-pricing ; CodeRabbit third-party: https://dev.to/rahulxsingh/coderabbit-pricing-in-2026-free-tier-pro-plans-and-enterprise-costs-1pc4 ; Greptile third-party: https://www.capterra.com/p/10050225/Greptile/reviews/ ; Graphite: https://graphite.com/pricing ; https://graphite.com/blog/introducing-graphite-agent-and-pricing ; Claude review cost: https://emelia.io/hub/claude-code-review-test
[S5b] Baz and Macroscope aggregator pricing: https://stackpick.net/pricing/baz/ ; https://stackpick.net/pricing/macroscope/
[S6] Benchmarks: https://codereview.withmartian.com/ ; https://github.com/Quriosity-agent/articles/blob/main/2026-02-27/martian-code-review-benchmark-en.md ; https://gitautoreview.com/blog/ai-code-review-benchmark-2026 ; https://www.augmentcode.com/tools/coderabbit-vs-greptile-vs-augment-cosmos ; https://particula.tech/blog/greptile-vs-coderabbit-vs-qodo-ai-code-review-2026
[S7] CodeRabbit funding: https://www.scalevp.com/press/coderabbit-raises--60-million-series-b--valuation-hits--550-million ; https://www.coderabbit.ai/newsroom ; https://sacra.com/c/coderabbit/ ; https://www.kucoin.com/news/flash/ai-code-review-platform-coderabbit-raises-143m-in-series-b-funding
[S8] Greptile runtime validation claim and tool roundup: https://quashbugs.com/blog/best-ai-code-review-tools ; https://devtoollab.com/blog/ai-code-review-tools ; https://www.tembo.io/blog/ai-code-review-tools
[S9] Merge-gating comparison (Codacy, competitor, 2026-10-26): https://blog.codacy.com/ai-code-review-tools-compared-2026-why-most-cant-safely-block-a-merge-and-what-code-governance-fixes ; https://dev.to/ugliai/copilots-code-review-cannot-block-your-merge-it-says-so-in-the-docs-hfm
[S10] Greptile funding: https://siliconangle.com/2025/09/23/greptile-bags-25m-funding-take-coderabbit-graphite-ai-code-validation/ ; https://techcrunch.com/2025/07/18/benchmark-in-talks-to-lead-series-a-for-greptile-valuing-ai-code-reviewer-at-180m-sources-say
[S11] Bugbot hosts: https://cursor.com/docs/bugbot ; https://cursor.com/docs/integrations/azure-devops ; https://cursor.com/docs/integrations/bitbucket ; https://cursor.com/docs/integrations/gitlab.md
[S12] CodeRabbit self-hosted: https://docs.coderabbit.ai/self-hosted/overview ; https://docs.coderabbit.ai/self-hosted/gitlab ; https://docs.coderabbit.ai/self-hosted/azure-devops ; https://docs.coderabbit.ai/self-hosted/bitbucket
[S13] Qodo Series B (2026-03-30): https://www.techcrunch.com/2026/03/30/qodo-bets-on-code-verification-as-ai-coding-scales-raises-70m/ ; https://siliconangle.com/2026/03/30/ai-generated-code-verification-startup-qodo-raises-70m/ ; https://www.qodo.ai/blog/qodo-70m-series-b-shift-to-artificial-wisdom/
[S14] Qodo handing PR-Agent to community (April 2026, per Atlas survey): https://laoujin.github.io/Atlas/research/2026-06-09-ai-assisted-code-review-in-2026-landscape-tools-evidence-and-limits/market-map-commercial-vendors-and-platforms/
[S15] Ellipsis: https://www-origin.ellipsis.dev/blog/usage-based-pricing ; https://costbench.com/software/ai-code-review/ellipsis/ ; https://tenki.cloud/blog/tenki-code-review-vs-ellipsis
[S16] Korbit: https://pitchbook.com/profiles/company/292654-00 ; https://codeant.ai/blogs/migrate-from-korbit-ai-to-codeant-ai
[S17] Baz funding and Planner (2026-06-29): https://siliconangle.com/2026/06/29/exclusive-agentic-coding-startup-baz-brings-code-reviews-planning-stage-extends-seed-funding-17m/
[S18] cubic: https://www.ycombinator.com/companies/cubic ; https://www.everydev.ai/developers/cubic ; https://www.startuphub.ai/startups/cubic
[S19] Macroscope funding (2025-09-17): https://lsvp.com/stories/the-end-of-micromanagement/ ; https://techcrunch.com/2025/09/17/meet-macroscope-an-ai-tool-for-understanding-your-code-base-fixing-bugs/
[S20] Gitar (2026-04-15) and Sonar: https://techcrunch.com/2026/04/15/gitar-a-startup-that-uses-agents-to-secure-code-emerges-from-stealth-with-9-million/ ; https://www.sonarsource.com/blog/announcing-gitar-the-future-of-code-validation/ ; https://www.sonarsource.com/plans-and-pricing/gitar/
[S21] Codex review: https://developers.openai.com/codex/integrations/github.md ; https://developers.openai.com/codex/pricing ; https://community.openai.com/t/codex-github-code-review-usage-increased-significantly-on-chatgpt-business-can-i-lower-the-model-or-control-review-token-usage/1383969
[S22] Gemini and Jules: https://tech-insider.org/ie/gemini-code-assist-shutdown-2026/ ; https://dev.to/jovan_chan_9500711396d4e6/gemini-code-assist-standard-vs-enterprise-2026-is-22month-worth-it-for-teams-2h1k ; https://www.techbloat.com/software/jules/
[S23] AWS: https://docs.aws.amazon.com/codeguru/latest/reviewer-ug/codeguru-reviewer-availability-change.html ; https://aws.amazon.com/blogs/devops/amazon-q-developer-end-of-support-announcement/ ; https://docs.aws.amazon.com/codeguru/latest/security-ug/end-of-support.html
[S24] Sonar: https://www.sonarsource.com/pricing ; https://docs.sonarsource.com/sonarqube-server/2026.1/ai-capabilities/overview
[S25] Semgrep, Snyk, Tabnine: https://semgrep.dev/pricing ; https://dev.to/rahulxsingh/is-snyk-worth-the-cost-complete-pricing-breakdown-for-2026-39m1 ; Atlas survey in S14
[S26] CodeAnt, Panto, Aikido: https://www.g2.com/products/codeant-ai-code-reviewer/pricing ; https://stackpick.net/pricing/panto-ai/ ; https://codeant.ai/blogs/aikido-security-pricing
[S27] Kodus: https://kodus.io/byo-llm-code-review/ ; https://github.com/kodustech/kodus-ai ; https://docs.kodus.io/how_to_use/en/pricing
[S27b] Proval: https://proval.app/
[S28] OSS comparison (Kodus blog, vendor-authored, release dates to 2026-10-05): https://kodus.io/en/open-source-ai-code-review-tools-compared/ ; https://kodus.io/self-hosted-ai-code-review/ ; https://github.com/miracodeai/mira
[S29] Augment Code Review: https://www.augmentcode.com/blog/introducing-augment-code-review ; https://www.augmentcode.com/pricing
[S30] Devin Review: https://cognition.com/blog/new-self-serve-plans-for-devin ; https://cognition.ai/blog/devin-review ; https://alphasignal.ai/news/cognition-slashes-devin-review-2-0-costs-by-82-per-pull-request
[S31] Factory: https://sacra.com/c/factory/ ; https://alphasignal.ai/news/factory-raises-200m-tripling-its-valuation-to-5b-in-five-months ; https://www.businesswire.com/news/home/20250925993478/en/Factory-Raises-50M-Series-B
[S32] GitLab Duo flat-rate review (2026-03-19): https://about.gitlab.com/blog/agentic-code-reviews-with-flat-rate-pricing

Not researched or not verified: JetBrains review features, Qodo Cover status, Bito and Sourcery host lists, Aikido AutoTriage price, Copilot code review non-GitHub hosts, Gitea support at any SaaS vendor besides Greptile Enterprise, cost per review for Heimdall on DeepSeek.
