# Heimdall: market and go-to-market research

Date: 2026-11-20. Method: about 35 web searches and 3 page fetches. Many sources are vendor blogs or price aggregators. Any number marked **not verified** has no primary source here, or sources conflict. Prices change often. Check each vendor page before you quote a number to a customer.

## 0. Summary

- The category is hot and well funded. CodeRabbit raised $143M at $1.5B in Aug 2026. Greptile raised $50M at $750M in Oct 2026. Qodo raised $70M in Mar 2026. Cursor bought Graphite in Dec 2025.
- The big players now use the same words as Heimdall. CodeRabbit sells "agentic change management", a "control layer". Qodo sells "code governance" and "trust". The word "gate" alone is not a wedge.
- Heimdall's real edge is the mix of four facts: self-hosted on one small VM, cheap models, a deterministic gate with human approval of the exact commit, and a hash-chained audit log.
- Recommended wedge: **change assurance with audit evidence for regulated teams that ship AI-written code and cannot upload code to a SaaS.** Sell to the platform or security lead who owns the SOC 2, ISO 27001, PCI DSS or DORA change-management evidence.
- Recommended price: open core, then $15 per active committer per month (Team) and about $35 (Regulated), with a floor of $20k per year on the top tier. BYO model key on all tiers.

## 1. Market size, growth and funding

### 1.1 Analyst sizes (paid reports, opaque methods, wide spread)

| Source | Scope | 2025 size | Forecast | CAGR |
|---|---|---|---|---|
| Market Intelo | AI code review | $1.4B | $10.8B (2034) | 28.5% |
| Dataintelo | AI-generated code review tools | $1.8B | $9.4B (2034) | 20.2% |
| Market Research Forecast | AI code review tool | $1.29B | $3.24B (2033) | 12.3% |
| The Business Research Company | Generative code review | $2.12B | $8.73B (2030) | 32.7% |
| LP Information / Global Info Research (2024 report) | AI code review tool | $734M (2024) | $1.27B (2030) | 9.5% |
| HHeuristics | AI code tools (broad) | $7.4B | $24.0B (2030) | 26.6% |
| Mordor Intelligence | Secure code review platforms | $1.22B | $2.44B (2030) | 14.9% |

Reading: a fair 2025 figure for AI code review alone is **$1.3B to $2.1B**, CAGR 9% to 33%. All of these are **not verified** against the report methods.

### 1.2 Adoption signals (why review is the bottleneck)

| Signal | Number | Source type |
|---|---|---|
| Share of code written by AI agents (per-respondent average) | 47% fully agent-generated | JetBrains survey, 15,000+ devs, 2026 |
| Share of committed code AI-generated or heavily assisted | 65% (2027 report) vs 42% (earlier report) | Sonar survey (vendor) |
| AI-authored merged code | about 30% (Q1) to about 50% median (Q2) | DX, 400+ to 500+ orgs, self-reported |
| Devs who think AI code is not functionally correct / who always check it | 96% / 48% | Sonar (vendor, sells verification) |
| Issues in AI co-authored PRs vs human-only PRs | about 1.7 times | CodeRabbit analysis (vendor) |
| Developers worldwide | 26.3M to 27M | Evans Data via secondary sites |

### 1.3 Funding and revenue (2025 to 2026)

| Company | Event | Amount and valuation | Revenue claim | Confidence |
|---|---|---|---|---|
| CodeRabbit | Series A, Aug 2024 | $16M (CRV) | $1M+ ARR | primary (own blog) |
| CodeRabbit | Series B, Sep 2025 | $60M, $550M valuation (Scale VP, NVentures, CRV) | $15M+ ARR, 8,000+ paying customers | TechCrunch via Techmeme |
| CodeRabbit | Series C, 12 Aug 2026 | $143M, $1.5B valuation (Atomico, Smash Capital) | own blog, 12 Nov 2026: $10M to $100M ARR in just over a year, 20,000+ customers, 400,000 monthly users, 3M+ PRs per week | company claim, not audited |
| Greptile | Series A, Sep 2025 | $25M (Benchmark), reported $180M valuation | not disclosed | own blog |
| Greptile | Series B, 19 Oct 2026 | $50M, $750M valuation (Menlo) | not disclosed | own blog, RuntimeWire |
| Graphite | Series B, Mar 2025 | $52M, $290M valuation (Accel) | n/a | TechCrunch |
| Graphite | Acquired by Cursor, 19 Dec 2025 | price not disclosed; Axios said well above $290M | n/a | Cursor blog, Fortune |
| Qodo | Series A, Sep 2024 | $40M | n/a | company |
| Qodo | Series B, 30 Mar 2026 | $70M (Qumra), total $120M | n/a | TechCrunch, SiliconANGLE |
| Cursor (Anysphere) | late 2025 round | $2.3B at $29.3B | $1B ARR by Nov 2025 | secondary |
| Cursor | 2026 | talks at $50B; a $60B all-stock SpaceX deal was reported | $2B ARR Feb 2026, about $4B May 2026 (Sacra estimate) | **not verified** (aggregator blogs) |

Notes. Sacra puts CodeRabbit near $50M ARR in July 2026. CodeRabbit says $100M by Nov 2026. These two do not conflict if growth was fast, but neither is audited. Qodo cites an F1 of 50.3% on Martian's Code Review Bench. TechCrunch cites 64.3%. The metrics differ, **not verified**.

Takeaway: investors paid about 2.7 times more for CodeRabbit in 11 months. The category will be crowded and loud. A small new entrant needs a narrow wedge, not a general "AI reviewer".

## 2. Pricing models and price points

### 2.1 AI review and code assurance vendors

| Vendor | Model | List price | Notes |
|---|---|---|---|
| CodeRabbit | per active PR author | Pro $24 per dev per month annual, $30 monthly. Pro+ $48 annual, $60 monthly | Only PR authors count as seats. A third source says a rename and a $72 tier started Sep 2026, **not verified** |
| CodeRabbit self-hosted | enterprise license | AWS Marketplace listing: $15,000 per month, 1-month contract | Listing price. Infra extra, third-party guess $500 to $8,000+ per month, **not verified** |
| Greptile | base plus usage (from Mar 2026) | $30 per dev per month incl. 50 reviews, then $1 per review | Base review 1 credit, Plus 3, Apex 10. Free for one dev (50 credits) |
| Graphite | per reviewed PR author | Team $40 per user per month annual, $50 monthly | Matched Bugbot at launch (Oct 2025) |
| Cursor Bugbot | was per seat, moving to usage | $40 per user per month (legacy). Usage model announced May 2026 | A third-party guide says about $1.00 to $1.50 per run, **not verified** |
| Qodo | per user, then pooled credits | Teams $30 per user annual ($38 monthly), older Enterprise from about $45. Jun 2026: Pro Team pooled-credit plan up to 30 users | On-prem and air-gapped only on custom Enterprise (30+ users), BYOK available |
| GitHub Copilot | seat plus usage | Business $19, Enterprise $39 per user per month (GitHub docs) | Code review draws AI credits ($0.01 each) and Actions minutes from 1 Jun 2026. Enterprise needs GitHub Enterprise Cloud on top |
| Claude Code Review (Anthropic) | per review, tokens | $15 to $25 per review typical | Billed to the org by usage credits. Secondary sources attribute the range to Anthropic docs |
| Semgrep | per contributor | Team $30 per contributor per month, free up to 10 | Other pages show $35 to $50, inconsistent |
| Snyk | per contributing dev | Team $25 per dev per month | Enterprise by quote. $15k+ per year is a third-party estimate |

### 2.2 Open-core and self-hosted enterprise licences

| Vendor | Self-hosted pricing signal | Confidence |
|---|---|---|
| GitLab | Premium $29 per user per month. Ultimate about $99 (unofficial). Self-managed costs the same as SaaS | Premium primary (handbook). Ultimate not verified |
| Sourcegraph | Enterprise $59 per user per month for 25+ devs (docs). Enterprise Starter $19 per user, up to 50 devs. One blog says a $16,000 per year minimum | Mixed, partly not verified |
| Mattermost | Professional $10, Enterprise $20 per user per month annual. Forum reports up to $30 | Third-party, not verified |
| Sentry | Core is open. Web apps use the Functional Source License. Self-hosted is community only. Team $26, Business $80 per month (hosted) | Third-party |
| Ona (Gitpod) | Credit-based (OCUs). Enterprise is private offer. One AWS listing shows $60,000 | Unclear |
| Coder | No pricing found | **not verified** |
| Semgrep, Snyk | See 2.1 | see above |

### 2.3 Patterns

- **Per active committer or PR author** is the standard for review tools (CodeRabbit, Graphite, Snyk, Semgrep). It is easy to explain and easy to audit.
- **Base plus usage** is the new move (Greptile, Qodo credits, Copilot AI credits, Bugbot). Buyers complain about unpredictable bills (Cursor forum, Codacy blog). A flat price with BYO key is a clear contrast.
- **Per review at token cost** (Claude Code Review at $15 to $25) shows what a deep multi-agent review costs at frontier-model prices. Heimdall's cheap models are 10 to 100 times lower per PR (section 7).
- **Self-hosted enterprise** is sold as a quote. Anchors: CodeRabbit $15k per month, Sourcegraph about $16k per year minimum (unverified), Snyk $15k+ per year (unverified), GitLab and Sentry at seat price.
- **BYO-key discount:** I found no published discount. Qodo offers BYOK on Enterprise. PR-Agent is free to self-host with your own key. Treat the discount as **not verified** and design it as a lower platform fee, not a price cut.

## 3. Buyer personas and triggers

| Persona | Pain | Trigger event | What they buy |
|---|---|---|---|
| Engineering manager | PRs pile up. Review quality falls as AI code grows | Review queue time doubles. A bad merge reaches prod | Speed and fewer misses. Wants low friction |
| Platform / DevEx lead | Wants one policy across repos. Hates plugin sprawl | Roll-out of coding agents to all teams | A gate that every repo and every agent must pass |
| CISO / AppSec | Does not know what code leaves the company or who approved what | Vendor review of an AI tool is refused. A security incident | Self-hosted control, evidence, no code upload |
| Compliance / GRC | Needs evidence for the auditor | SOC 2 or ISO audit window. A finding on change management | Exportable proof per change |
| Head of engineering in a bank, insurer, health firm | Regulator asks how AI code is controlled | DORA, PCI audit, internal model-risk review | An auditable, on-prem process |

### 3.1 Which controls ask for review or test evidence before a production change

| Framework | Control | What it needs | Fit with Heimdall |
|---|---|---|---|
| SOC 2 (AICPA TSC) | CC8.1 change management | Changes authorized, designed, tested and approved before release. Auditors sample PRs. Common finding: author approves own change, or approval is older than the last commit | Gate blocks self-approval. Approval binds to the exact commit SHA |
| ISO/IEC 27001:2022 | Annex A 8.32 change management, 8.29 security testing in development and acceptance | Documented change procedure and tests before acceptance. Controls are picked by risk and recorded in the Statement of Applicability, so they are not automatic | Red-first tests and review records are evidence for both |
| PCI DSS 4.0 | 6.2.3 review of bespoke code before release, 6.2.3.1 manual review by someone other than the author, 6.5.1 change control records | Review before release, independent reviewer, approval, testing, rollback plan | Independent AI review plus human approval. Rollback plan is not in Heimdall today |
| DORA (EU) | RTS 2024/1774 on ICT risk management, in force 17 Jan 2025 | Changes tested, quality-assured and approved. Approvers and implementers are separate roles. Source code review, static and dynamic. Open-source code analysed before production. Article numbers not confirmed | Separation of duties maps well. Dynamic testing is partly covered |
| NIST SSDF SP 800-218 | PW.7 (review or analyse code), PW.8 (test executable code) | Per my memory, **not verified** in this research | Same evidence set |
| US secure software attestation form (CISA, Mar 2024, from OMB M-22-18 and M-23-16) | Self-attestation of SSDF practices by a vendor to federal agencies | A CEO or equivalent signs. 2026 status **not verified** | Evidence for a signer |
| FedRAMP | 20x consolidated rules CR26 released 25 Jun 2026. Phase 4 from 1 Oct 2026. Rev5 new applications stop 11 Jun 2027 | NIST 800-53 CM-3 style change control, per my memory, **not verified** | Fit is real but long cycle |
| HIPAA Security Rule | No explicit code-review rule that I could confirm | **not verified**. Evidence helps risk analysis only | Weak direct pull |
| EU AI Act | Regulates AI systems, not AI-written code. Annex III high-risk duties are deferred to 2 Dec 2027 under the Digital Omnibus (a July 2026 law-firm note treats it as in force; formal adoption **not verified**). Annex I to 2 Aug 2028 | Logging and human oversight duties apply to the customer's high-risk AI product, not to its code review tool | Indirect. Do not sell it as an AI Act product |

### 3.2 AI-written code and audit

- No AI-specific SOC 2 standard exists. The AICPA has not published one. Auditors apply the same change controls (vendor blogs, **not verified** as authoritative).
- Auditors ask for proof that a human reviewed AI code. Signals they flag: a large change approved in minutes, approval before the last commit, author equals approver.
- A good evidence chain is PR, approved head SHA, merge SHA, deploy. This is exactly what "human approval of the exact commit" and a hash-chained log produce.
- AI-made summaries are not evidence. Source records are. Heimdall keeps source records.

## 4. Segments that need self-hosted or air-gapped tools

| Segment | Why self-host | Size signal | Examples | Confidence |
|---|---|---|---|---|
| Regulated finance | Data rules, DORA, PCI, model-risk teams | No count of banks that block SaaS review found | Goldman Sachs restricted advanced AI coding tools to engineers only (Bloomberg, 2 Nov 2026). JPMorgan required about 65,000 tech staff to use approved AI tools and set Claude Code spend limits | News, not a market size |
| Defense and aerospace | CUI, CMMC, export control | About 200,000 to 221,000 defense industrial base firms (GAO, DoD), 74% small. About 1,700 had CMMC Level 2 by Jun 2026. CMMC Phase II was suspended 13 Jul 2026 | Primes and suppliers | Counts conflict. Dev count not verified |
| Government | FedRAMP, sovereignty | FedRAMP 20x timeline above | Federal agencies and integrators | not verified |
| EU sovereignty | Data Act, GDPR, NIS2, DORA | Gartner: sovereign IaaS $59.3B (2025), $80.4B (2026), $110.6B (2027). MarketsandMarkets: EU $12.6B in 2026, 15% to 40% price premium | EU banks, insurers, public sector | Infra spend, not dev tools |
| Healthcare | PHI rules | No sizing found | Hospital IT, health SaaS | **not verified** |
| "No SaaS upload" companies | Policy of the CISO | No survey found | Unknown, found by sales | **not verified** |

My bottom-up guess, clearly an estimate: 27M developers, if 3% to 5% work where code cannot go to a SaaS, that is 0.8M to 1.35M developers. At $15 to $35 per month, that is $150M to $570M per year of reachable licence spend. **Not verified.** The figure is only a size check, not a forecast.

Fact to watch: on-prem competitors exist. CodeRabbit lists a self-hosted option on AWS Marketplace. Qodo sells on-prem and air-gapped on Enterprise. Sonar and GitLab are already on-prem in many regulated firms. Heimdall must win on cost, small footprint and evidence quality, not on "self-hosted" alone.

## 5. Distribution channels that worked

| Channel | Concrete evidence | Number | Fit for Heimdall |
|---|---|---|---|
| Free for open source, then paid for private repos | CodeRabbit | 100,000+ OSS users and 150,110 GitHub Marketplace installs (Sacra, self-reported), 300,000+ OSS projects (Pulse2), $10M+ pledged to OSS. 8,000+ paying customers in Sep 2025, 20,000+ in Nov 2026 | Strong pattern, but Heimdall runs on a VM. OSS maintainers rarely run one |
| GitHub Marketplace | CodeRabbit is called #1 AI app | 16K+ installs in Aug 2024, 150K installs later (Sacra) | Hosted apps fit. A self-hosted gate lists only as a docs page |
| AWS Marketplace | CodeRabbit self-hosted $15k per month listing. AWS private-offer promotion (Aug 2025) gives buyers AWS credits. Express private offers (Nov 2025) | No devtools-wide sales figure found, **not verified** | Good for enterprise spend that is already approved. Needs a listing and a container image |
| Show HN | Preprint on 138 AI repos: about 121 stars in 24 h. Blog on 188,000 posts: about 1.4 stars per upvote | Low hundreds of stars. One maintainer case: 736 stars in a month | Cheap, good for early trust. Stars do not equal revenue |
| Open core path | Practitioner blogs: free-to-paid about 1% to 5% (unsourced). Preliminary study of 44 devtools: stars link to funding, revenue model matters more | **not verified** | Use the free tier to find teams, not to count stars |
| Product Hunt | Category page exists. No launch data found | **not verified** | Low value for a B2B self-hosted tool |
| X build-in-public, Reddit | No usable numbers found | **not verified** | Use for posts about the AI-slop problem and audit evidence, not for sales |
| Maintainer programs | Greptile (apply, non-commercial), Qodo (free for public GitHub repos), CodeRabbit (public repos free), Sourcery (public free), Mergify (free OSS) | n/a | Table stakes if you chase OSS |

Context for a maintainer play: curl ended its bug bounty in Jan 2026 over AI reports. Ghostty limited AI contributions to pre-approved issues. tldraw auto-closes outside PRs. Godot maintainers call AI PRs "draining". GitHub considered a way to turn off PRs. The pain is real, but the buyers are unpaid volunteers.

## 6. Positioning options

Scores: 1 (weak) to 5 (strong). "Fit" means fit with what Heimdall already has.

| Option | Pain | Willingness to pay | Competition (5 = open field) | Fit with built product | Notes |
|---|---|---|---|---|---|
| A. Cheapest self-hosted AI reviewer | 3 | 2 | 1 | 4 | Cheap models help. But CodeRabbit, Qodo, PR-Agent (free, self-host) and Greptile already exist. A price war with funded firms is a bad start |
| B. Merge gate and change assurance with compliance evidence | 4 | 4 | 3 | 5 | Pain is clear (CC8.1, PCI 6.2.3.1, DORA). Buyer has budget. CodeRabbit and Qodo now use the same words, so proof must be concrete |
| C. AI slop firewall for open source | 4 | 1 | 2 | 2 | Real pain. Buyers do not pay. Needs a hosted or Action form. Good for reputation only |
| D. Test-first agent (red-first tests from the ticket) | 3 | 3 | 3 | 5 | A differentiator inside B. Alone it competes with coding agents |
| E. Sovereign / air-gapped AI governance for regulated firms | 4 | 4 | 3 | 3 | Same buyer as B. Needs local model support and an offline bundle. Not built yet (not verified in repo) |

**Winner: B, with E as the first vertical and D as the proof of rigor.** Reason: B has the best mix of paid pain and product fit. The buyer already has an audit deadline.

### 6.1 Proof Heimdall must show against "control layer" claims

1. The final decision is a deterministic gate, not a model opinion.
2. A human approves the exact head SHA. A new commit voids the approval. (Policy is read from the merge base, not the PR head.)
3. Every change has a hash-chained event log that an auditor can verify offline.
4. Tests are written red-first from the ticket by an agent that does not write the code.
5. One VM, 2 vCPU and 16 GB, server near 42 MiB RAM. Code never leaves the company. BYO key or local model.

### 6.2 Risks

- A funded rival can copy the "evidence" page in a quarter. Keep the lead through audit-pack depth (mapping to named controls) and design partners.
- Heimdall's own live run (H11) showed reviewers that ran out of output tokens and an unverified provider balance. Reliability must be fixed before a paid pilot.
- Cursor and GitHub can bundle review for free. Self-hosted and audit evidence are the places bundles do not reach.

## 7. Unit economics sketch

### 7.1 Model cost per PR

Heimdall's own data (docs/PROGRESS.md, H11): one live journey used 278k input tokens, 5.59M cache-read tokens and 173k output tokens, about **$0.24** (estimated from OpenCode token counts, provider balance not read). Only one of three reviews finished. The doc states an expected $0.30 to $1.00 per full run. I use **$0.25 to $1.00 per PR** as the planning range. **Not verified** for a full run.

Check with list prices (DeepSeek, April 2026, secondary sources):

| Model | Input per 1M | Output per 1M | Cost of 0.5M in + 0.1M out |
|---|---|---|---|
| DeepSeek V4 Flash | $0.14 | $0.28 | $0.10 |
| DeepSeek V4 Pro (list) | $1.74 | $3.48 | $1.22 |
| DeepSeek V4 Pro (OpenRouter, promo) | $0.435 | $0.87 | $0.31 |

OpenRouter rates vary by host. The 0.5M and 0.1M token mix is my assumption.

### 7.2 Compare with per-seat and per-PR prices (50 developers)

Assume 20 PRs per dev per month, so 1,000 PRs per month. This is my assumption, **not verified**. AI-heavy teams may reach 2,000.

| Option | Monthly cost for 50 devs | Per PR (1,000 PRs) | Basis |
|---|---|---|---|
| Heimdall model cost only | $100 to $1,000 | $0.10 to $1.00 | Section 7.1 |
| Copilot Business seats | $950 plus Actions minutes and AI credits | about $0.95+ | $19 per user |
| CodeRabbit Pro | $1,200 (annual) to $1,500 | $1.20 to $1.50 | $24 or $30 per dev |
| CodeRabbit Pro+ | $2,400 to $3,000 | $2.40 to $3.00 | $48 or $60 |
| Greptile | $1,500 plus $1 per review above 50 per dev | $1.50+ | $30 per dev |
| Graphite Team | $2,000 to $2,500 | $2.00 to $2.50 | $40 or $50 |
| Claude Code Review | $15,000 to $25,000 | $15 to $25 | Per-review range |
| CodeRabbit self-hosted | $15,000 | $15 | AWS Marketplace listing, 500+ users implied (not verified) |

### 7.3 VM cost for the team

| Host | Spec | Monthly price | Source note |
|---|---|---|---|
| AWS t3.large | 2 vCPU, 8 GiB | about $61 on demand | $0.0832 per hour, third-party aggregator |
| AWS t3.xlarge | 4 vCPU, 16 GiB | about $121 | $0.1664 per hour |
| AWS m7i.xlarge | 4 vCPU, 16 GiB | about $147 | $0.2016 per hour |
| AWS, exact 2 vCPU / 16 GiB | memory-optimised class | not priced here | **not verified** |
| Hetzner CX43 | 8 vCPU, 16 GB | EUR 15.99 ex VAT after 15 Jun 2026 (was 11.99) | Hetzner docs. Some CX plans were out of stock |

Spot prices are far lower, but a merge gate should not run on a spot VM. Add storage, backups and Postgres care. I assume $20 to $60 per month, **not verified**.

### 7.4 Total cost of ownership

50 developers: VM $75 to $150, model $100 to $1,000, licence $750 (at $15 per active committer). Total **$925 to $1,900 per month**, or $18 to $38 per developer. Compare with CodeRabbit Pro at $24 to $30 and Pro+ at $48 to $60 per dev, plus Heimdall's gate and audit trail. The price is level with the market, and it buys self-hosting and evidence. Heimdall's gross margin is high because the customer pays for the VM and the tokens. The risk is support cost, not compute.

For the air-gapped tier, local models need a GPU box. That cost is **not verified** and is not in this sketch.

## 8. Recommended positioning and pricing

### 8.1 Positioning statement

"Heimdall is the self-hosted merge gate that gives auditors proof of every change: tests first, independent review, and a human approval tied to the exact commit. Your code never leaves your network."

One-line version for engineers: "Nothing merges unless the evidence is there. It runs on one VM."

First buyers: platform or security leads at regulated firms with 30 to 500 developers that already use coding agents. Examples of fit: fintech and payment firms (PCI 6.2.3.1), EU financial entities (DORA), SOC 2 SaaS firms with an audit due.

### 8.2 Proposed tiers (a proposal, not market-tested)

| Tier | Price | Includes |
|---|---|---|
| Community | Free, source-available or open core (licence choice is a separate decision) | Gate, red-first tests, AI review, hash-chained log, up to 5 active committers, 1 repo set, BYO key |
| Team | $15 per active committer per month, annual, 10 minimum | Unlimited repos, Jira and chat bridges, policy-as-code, audit export (CSV and JSON), email support |
| Regulated | $35 per active committer per month, $20,000 per year floor | Control-mapped evidence packs (SOC 2 CC8.1, ISO 8.32 and 8.29, PCI 6.2.3 and 6.5.1, DORA), SSO, signed log export and offline verifier, offline install bundle, local-model support, support SLA |
| Design partner | $5,000 flat for 90 days, credited to year one | Hands-on setup. Needed to close reference customers |

Rules:
- "Active committer" means a person whose PR passed through the gate in the last 90 days. This matches Snyk's rule and CodeRabbit's "PR author" rule.
- BYO key is the default. Offer a managed key as an add-on only if a buyer asks. Do not resell tokens at first.
- Keep list price flat per committer. Buyers dislike surprise usage bills (Cursor forum, Codacy blog).
- Free for verified open-source maintainers, but ship it only after a Github Action or hosted mode exists. Until then, skip the claim.

### 8.3 First 90 days

1. Fix the reliability gaps from the H11 run (reviewer output limits, loop on failed seats) before any paid pilot.
2. Write the "auditor pack": one page per framework control with a sample log export. Test it with one SOC 2 auditor or GRC consultant.
3. Find 3 design partners in fintech or EU finance through direct outreach. Offer the $5,000 pilot.
4. Publish a Show HN and a short demo of a tampered log failing verification. Expect low hundreds of stars, not leads.
5. Prepare the AWS Marketplace listing (container plus private offers) for the first enterprise deal.
6. Measure: time from install to first gated PR, cost per PR, number of changes with complete evidence.

## 9. Gaps and not verified

- HIPAA, FedRAMP CM-3 and NIST SSDF PW.7 mappings come from memory or thin sources.
- DORA article numbers. Check Regulation (EU) 2024/1774 in the Official Journal.
- EU AI Act omnibus adoption date and Article 12 text.
- Cursor valuation and SpaceX deal, CodeRabbit ARR, Greptile revenue.
- BYO-key discounts, Coder pricing, Sentry and Mattermost self-hosted list prices.
- Any count of firms that ban SaaS code review. No survey found.
- Hetzner and AWS price for a 2 vCPU, 16 GB host. The reference VM class was not priced.
- Product Hunt, X and Reddit launch numbers.

## Sources

Funding and revenue
- CodeRabbit Series B (Techmeme/TechCrunch): https://www.techmeme.com/250916/p50
- CodeRabbit Series B (Business Wire): https://www.businesswire.com/news/home/20250916401011/en/CodeRabbit-Raises-%252460M-Series-B-Following-Unprecedented-Growth-as-Vibe-Coding-Triggers-a-Need-for-New-Code-Quality-Standards
- CodeRabbit Series C, Atomico note: https://www.coderabbit.ai/newsroom/atomico-investment-coderabbit-agentic-software-development
- CodeRabbit Series C (Yahoo Finance): https://finance.yahoo.com/technology/ai/articles/coderabbit-raises-143m-series-c-090741516.html
- CodeRabbit Agentic Change Management: https://www.coderabbit.ai/blog/introducing-agentic-change-management
- CodeRabbit $100M ARR post: https://coderabbit.ai/blog/coderabbit-100-million-arr
- CodeRabbit Series A post: https://www.coderabbit.ai/blog/coderabbit-announces-16m-series-a-funding-led-by-crv
- CodeRabbit ARR tracker (Sacra): https://sacra.com/research/coderabbit
- CodeRabbit free OSS tier and Marketplace (Sacra): https://sacra.com/chat/h/a661946e-2d95-4c66-a423-236803077665/
- CodeRabbit OSS pledge: https://coderabbit.ai/blog/we-are-committed-to-supporting-open-source-distributed-600000-to-open-source-maintainers-in-2025
- Greptile Series A: https://www.greptile.com/blog/series-a
- Greptile Series B: https://www.greptile.com/blog/series-b
- Greptile Series B (RuntimeWire): https://runtimewire.com/article/greptile-raises-50m-series-b-at-750m-valuation
- Cursor buys Graphite (Cursor): https://cursor.com/blog/graphite
- Cursor buys Graphite (TechCrunch): https://techcrunch.com/2025/12/19/cursor-continues-acquisition-spree-with-graphite-deal
- Cursor buys Graphite (Fortune): https://fortune.com/2025/12/19/cursor-ai-coding-startup-graphite-competition-heats-up/
- Qodo Series B (TechCrunch): https://www.techcrunch.com/2026/03/30/qodo-bets-on-code-verification-as-ai-coding-scales-raises-70m/
- Qodo Series B (SiliconANGLE): https://siliconangle.com/2026/03/30/ai-generated-code-verification-startup-qodo-raises-70m/
- Cursor funding (Sacra): https://sacra.com/c/cursor
- Cursor funding (Next Web): https://thenextweb.com/news/cursor-anysphere-2-billion-funding-50-billion-valuation-ai-coding

Market size
- Market Intelo: https://marketintelo.com/report/ai-code-review-market
- Dataintelo: https://dataintelo.com/report/ai-generated-code-review-tools-market
- Market Research Forecast: https://www.marketresearchforecast.com/reports/ai-code-review-tool-15114
- The Business Research Company: https://www.thebusinessresearchcompany.com/report/generative-code-review-global-market-report
- Global Info Research (MarketResearch.com): https://www.marketresearch.com/GlobalInfoResearch-v4117/Global-AI-Code-Review-Tool-43270184
- Mordor Intelligence: https://www.mordorintelligence.com/industry-reports/secure-code-review-platforms-market
- JetBrains research on agent-written code: https://blog.jetbrains.com/research/2026/08/how-much-code-do-developers-really-let-agents-write/
- Sonar State of Code survey: https://www.sonarsource.com/blog/state-of-code-developer-survey-report-the-current-reality-of-ai-coding/
- DX AI-authored code: https://getdx.com/blog/ai-generated-merged-code-holds-steady-at-30/
- The Register on trust in AI code: https://www.theregister.com/2026/01/09/devs_ai_code/
- Developer count (Grid Dynamics, citing Evans Data): https://www.griddynamics.com/blog/number-software-developers-world

Pricing
- CodeRabbit plans: https://docs.coderabbit.ai/management/plans
- CodeRabbit on AWS Marketplace: https://aws.amazon.com/marketplace/pp/prodview-wkkkre4fgelwq
- CodeRabbit pricing guide: https://dev.to/rahulxsingh/coderabbit-pricing-in-2026-free-tier-pro-plans-and-enterprise-costs-1pc4
- Greptile v4 and pricing: https://www.greptile.com/blog/greptile-v4
- Greptile pricing: https://www.greptile.com/pricing
- Graphite Agent pricing: https://graphite.com/blog/introducing-graphite-agent-and-pricing
- Cursor Bugbot May 2026 changes: https://cursor.com/blog/may-2026-bugbot-changes
- Cursor Bugbot legacy pricing: https://cursor.com/docs/bugbot/legacy-pricing
- Qodo pricing: https://qodo.ai/pricing/
- Qodo pricing summary: https://dev.to/rahulxsingh/qodo-ai-pricing-free-vs-teams-vs-enterprise-plans-in-2026-2mh5
- Qodo pooled credit change: https://usagepricing.com/blueprint/activity/qodo-2026-06-30-pooled-credit-model
- Copilot plans (GitHub docs): https://docs.github.com/en/copilot/tutorials/roll-out-at-scale/assign-licenses/choose-enterprise-plan
- Copilot usage billing: https://docs.github.com/en/copilot/concepts/billing/usage-based-billing-for-organizations-and-enterprises
- Copilot code review and Actions minutes: https://github.blog/changelog/2026-04-27-github-copilot-code-review-will-start-consuming-github-actions-minutes-on-june-1-2026/
- Claude Code Review costs: https://code.claude.com/docs/en/code-review-costs
- Codacy on per-PR cost: https://blog.codacy.com/ai-code-review-cost-per-pull-request-what-engineering-teams-actually-pay-in-2026
- GitLab pricing handbook: https://handbook.gitlab.com/handbook/company/pricing
- Sourcegraph enterprise pricing: https://sourcegraph.com/docs/pricing/enterprise
- Sourcegraph Enterprise Starter: https://sourcegraph.com/changelog/enterprise-starter
- Sourcegraph pricing summary: https://www.activepieces.com/blog/sourcegraph-pricing-2026-plans-costs-and-minimums
- Mattermost pricing: https://www.activepieces.com/blog/mattermost-pricing-plans-costs-free-tier-2026
- Sentry licensing: https://open.sentry.io/licensing
- Sentry pricing summary: https://www.activepieces.com/blog/sentry-pricing-2026-developer-team-business-plans
- Semgrep pricing: https://semgrep.dev/pricing
- Snyk pricing: https://snyk.io/pricing/
- Ona/Gitpod pricing: https://www.plushcap.com/content/ona/blog/gitpod-understanding-gitpod-pricing-tiers
- DeepSeek API pricing: https://api-docs.deepseek.com/quick_start/pricing
- DeepSeek V4 pricing summary: https://dev.to/hassann/deepseek-v4-api-pricing-2j8f
- DeepSeek V4 Pro on OpenRouter: https://openrouter.ai/models/deepseek/deepseek-v4-pro-0813
- AWS m7i.xlarge price: https://www.doit.com/compute/spot/us-east-1/m7i.xlarge
- AWS t3.xlarge price: https://www.doit.com/compute/spot/us-east-1/t3.xlarge
- AWS t3.large price (m7i.large page lists related sizes): https://www.doit.com/compute/spot/us-east-1/m7i.large
- Hetzner price adjustment: https://docs.hetzner.com/general/infrastructure-and-availability/price-adjustment/

Compliance and segments
- SOC 2 CC8.1 (ISMS.online): https://www.isms.online/soc-2/controls/change-management-cc8-1-explained/
- SOC 2 change management (Sprinto): https://sprinto.com/blog/soc-2-change-management/
- GitHub PRs as SOC 2 evidence: https://oneuptime.com/blog/post/2026-08-04-github-pull-requests-change-management-evidence/markdown
- AI code and SOC 2: https://crashoverride.com/resources/knowledge-base/compliance/soc-2-ai-coding-tools
- AI code review and SOC 2 Type 2: https://ysecurity.io/blog/agentic-sdlc-code-review-soc-2/
- ISO 27001 A.8.32: https://www.isms.online/iso-27001/annex-a-2022/8-32-change-management-2022/
- ISO 27001 A.8.29: https://www.isms.online/iso-27001/annex-a-2022/8-29-security-testing-in-development-acceptance-2022/
- PCI DSS 6.2.3.1 mapping: https://ce.prod.cloudaware.com/frameworks/pci-dss-v4.0/06/02/03/01/
- PCI DSS 4.0 and AppSec: https://scworld.com/resource/pci-dss-4-0-what-changes-for-appsec
- DORA change management (MFSA): https://www.mfsa.mt/publication/ict-change-management-under-the-digital-operational-resilience-act-dora/
- DORA RTS (EBA): https://eba.europa.eu/activities/single-rulebook/regulatory-activities/operational-resilience/regulatory-technical-standards-ict-risk-management-framework-and-simplified-ict-risk-management
- CISA secure software attestation form: https://www.cisa.gov/resources-tools/resources/secure-software-development-attestation-form
- FedRAMP 20x timeline: https://fedramp.gov/20x/timeline/
- FedRAMP CR26 announcement: https://www.fedramp.gov/2026-06-25-propelling-change-fedramp-launches-consolidated-rules-for-2026/
- EU AI Act omnibus (Orrick): https://www.orrick.com/en/Insights/2026/07/EU-AI-Act-Update-Digital-Omnibus-Finalizes-8-Compliance-Changes
- EU AI Act omnibus (Pinsent Masons): https://www.pinsentmasons.com/out-law/news/rules-high-risk-ai-delayed-under-eu-omnibus-deal
- Goldman Sachs AI coding restriction: https://www.roic.ai/news/goldman-sachs-restricts-advanced-ai-coding-tools-to-engineers-curbing-banker-access-11-02-2026
- Coder on financial services AI governance: https://coder.com/blog/financial-services-has-an-ai-governance-problem
- JPMorgan Claude Code limits: https://cryptobriefing.com/jpmorgan-claude-code-spending-limits/
- Europe sovereign cloud spend (The Register): https://www.theregister.com/2026/02/09/europe_sovereign_cloud_spend/
- Sovereign cloud (TechRadar, Gartner figures): https://www.techradar.com/pro/europe-is-going-all-in-on-sovereign-cloud-investment-to-triple-by-2027
- CMMC pause (CSIS): https://www.csis.org/analysis/what-cmmc-pause-means-defense-industrial-base
- Defense base compliance (Help Net Security): https://helpnetsecurity.com/2026/06/30/federal-cybersecurity-compliance-report/
- GAO on DIB: https://files.gao.gov/reports/GAO-26-107955/index.html

Distribution and positioning
- Show HN and GitHub stars (preprint): https://arxiv.org/html/2511.04453v1
- Show HN by the numbers (blog): https://danfking.github.io/blog/2026/04/23/show-hn-by-the-numbers/
- Hacker News effect (2019 paper): https://arxiv.org/pdf/1908.04219
- Open source growth strategy: https://daily.dev/blog/open-source-growth-strategy-enterprise-adoption/
- AWS Marketplace private offer promotions: https://aws.amazon.com/about-aws/whats-new/2025/08/aws-marketplace-private-offer-promotions
- AWS Marketplace express private offers: https://aws.amazon.com/about-aws/whats-new/2025/11/aws-marketplace-express-private-offers
- Product Hunt code review category: https://www.producthunt.com/categories/code-review-tools
- Godot and AI slop (The Register): https://www.theregister.com/2026/02/18/godot_maintainers_struggle_with_draining/
- GitHub PR kill switch (The Register): https://www.theregister.com/2026/02/03/github_kill_switch_pull_requests_ai/
- AI slop in open source (LeadDev): https://leaddev.com/software-quality/open-source-has-a-big-ai-slop-problem
- AI slop and big projects (Help Net Security): https://helpnetsecurity.com/2026/10/26/linux-foundation-open-source-ai-slop
- Greptile open source program: https://www.greptile.com/open-source
- Qodo open source program: https://docs.qodo.ai/open-source-program
- Sonar AI code governance: https://www.sonarsource.com/solutions/code-governance/
- Heimdall H11 cost evidence: docs/PROGRESS.md in this repo (row "H11 Deploy and live run")
