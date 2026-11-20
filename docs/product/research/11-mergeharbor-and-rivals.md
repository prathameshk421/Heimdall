# 11. MergeHarbor and the proof-run rivals (Ito, Heal, edgeverdict)

Research date: 2026-11-21. Read only. Method: vendor pages read directly (the site serves `.md` copies of its pages), `gh` read-only, web search. Anything I could not read is marked "not verified". Vendor claims about themselves are marked "vendor claim". Research 01 (`01-competitors.md`, dated 2026-11-20) did not cover any product in this note except Greptile's runtime validation, which it only had from secondary sources.

## 0. Bottom line

1. MergeHarbor is real, and the critic's feature claims are true as written in its own docs. This is a direct competitor to Heimdall's proof story, not a vague one.
2. It is also brand new and unproven. The domain was registered 2026-10-18. The GitHub account is from 2026-10-21. The three repos have 0 stars. The VS Code extension shows 1 install. No founders, customers, funding, launch posts or user complaints could be found.
3. Heimdall is not unique on: proof by failing test, a sandbox, a merge gate, "proven findings block", policy read from the merge base, agent rule files read from the merge base, break-glass override, advisory (shadow) mode, self-hosting, and audit evidence.
4. Heimdall's sharpest difference that I can defend: **a finding with no proof is dropped, and the drop count is printed, including findings made by other reviewers (CodeRabbit, Copilot, Bugbot).** MergeHarbor posts findings its own verifier model confirmed, and uses a proof run to raise confidence. Its gate reads a model confidence score. Its docs describe proving only its own findings. Details in section 6.
5. Heimdall is behind on almost everything else: hosts (1 vs 11), languages (Go only vs 8 stacks), a published benchmark, SSO and SCIM, a sealed audit ledger, IDE, CLI and MCP tools, a hosted option, and docs.

## 1. MergeHarbor: what it is

| Item | Finding | Source and date |
|---|---|---|
| What | "AI code review for engineering organizations". Review, triage, security scans, vulnerability management, Insights, a coding agent (Shipyard) and a chat agent. | https://mergeharbor.com/ (read 2026-11-21) |
| Domain age | Registered 2026-10-18 (Cloudflare registrar). | `whois mergeharbor.com`, run 2026-11-21 |
| Docs | About 150 pages, one per feature and per host. Page list in the sitemap, last modified 2026-11-01. | https://mergeharbor.com/sitemap-0.xml (2026-11-21) |
| Changelog | Four dated entries (2026-10-22, 10-23, 10-27, 11-01) and one undated "October 2026 General availability" entry. | https://mergeharbor.com/changelog (2026-11-21) |
| Blog | One post, dated 2026-10-17, on model routing and cost. | https://mergeharbor.com/blog (2026-11-21) |
| Founders | Not named on the site. The About page has principles and a design-partner call, no names. | https://mergeharbor.com/about (2026-11-21) |
| Funding | Not found anywhere. | Web search, 2026-11-21. Not verified |
| Customers | None named. The home page has no logos or testimonials (0 matches for "trusted by", "testimonial", "case study" in the page source). | https://mergeharbor.com/ (2026-11-21) |
| Launch posts | No Hacker News, Product Hunt or X post found. Searches returned other products with similar names (mrge.io, MergeStorm, Merge assessments). | Web search, 2026-11-21. Not verified |
| GitHub | The account `mergeharbor` is a user account, not an org. 3 repos, all 0 stars: `mh` (CLI release downloads, created 2026-10-20, releases v0.1.0 on 2026-10-20 and v0.1.1 on 2026-10-22), `homebrew-tap` (created 2026-10-20), `integrations` (created 2026-10-22, last push 2026-10-30, plugins for Claude Code, Codex, Cursor, no issues). Source is closed. | `gh api users/mergeharbor`, `gh repo view mergeharbor/integrations`, `gh release list -R mergeharbor/mh` (2026-11-21) |
| Related account | The repo `tastaslim/mergeharbor-qa-demo` ("test data, not production code", created 2026-10-21) belongs to GitHub user `tastaslim`, who also appears as owner of the `mh` repo path in search results. This suggests `tastaslim` is the maintainer or an early team member. It does not prove a founder role. Not verified. | `gh repo view tastaslim/mergeharbor-qa-demo`; https://github.com/tastaslim/mh (2026-11-21) |
| VS Code extension | 1 install, 1 rating. | https://marketplace.visualstudio.com/items?itemName=mergeharbor.mergeharbor (2026-11-21) |
| Security posture | "Does not yet have a SOC 2 report"; a Type I audit is "planned". | https://mergeharbor.com/security (2026-11-21) |
| Own benchmark | A pilot of 40 PRs (34 real bugs in 33 PRs, plus 7 clean changes) run on 2026-10-21. Standard tier: 91.2% of bugs caught, 0% of clean changes commented, $0.04 per review, 18 s median. The page says it is in-sample, tuned on that set, with no confidence intervals. The Max tier hit its $15 spending cap and reviewed only 1 of 7 clean changes. Vendor-run. | https://mergeharbor.com/benchmarks (2026-11-21) |

Reading: the product surface is wide and well documented for something about a month old. I cannot tell from public sources whether it has any paying user. Treat the docs as a spec of intent, not proof of a product in use.

## 2. The critic's claims, checked

| Claim | Verdict | Primary source (read 2026-11-21) |
|---|---|---|
| Went GA on Oct 23 2026 | PARTLY TRUE. The changelog says "October 2026, General availability". It gives no day. The entry dated Oct 23 is about contact-page topics. The exact date is NOT FOUND. | https://mergeharbor.com/changelog |
| Proof runs | TRUE. Documented, Team plan and up, off by default. | https://mergeharbor.com/docs/proof-runs ; https://mergeharbor.com/features/proof-runs |
| Merge gate where "proven findings: block" | TRUE. The config key is `proven_findings: block` (also `warn`, `off`). Default for the key is `block`. The gate is advisory by default, so it blocks nothing until a team switches `mode: enforce` and makes the check required. | https://mergeharbor.com/docs/configuration ; https://mergeharbor.com/docs/merge-gate |
| A `regulated` preset | TRUE. Presets: Relaxed, Standard (default), Strict, Regulated. Regulated: coverage, test gaps and high risk block; needs 2 approvals; "expects a proof run". The `proof_run` rule key defaults to `"off"`. | https://mergeharbor.com/docs/merge-gate ; https://mergeharbor.com/docs/configuration |
| Compliance evidence | TRUE, Advanced and Enterprise plans. Sealed per-change records in an append-only hash-chained ledger, SOC 2 / ISO 27001 / PCI DSS starter packs, policy-as-code in YAML, auditor reports with a seeded random sample and a signed SHA-256 manifest. Approvals and merger are read only from GitHub and GitLab; on other hosts approval controls fail instead of passing. | https://mergeharbor.com/compliance |
| Self-hosting and on-prem | TRUE, Enterprise plan. Helm (EKS, GKE, AKS, OpenShift), Docker Compose on one VM, AWS Terraform, air-gapped with vLLM or Ollama. Needs a signed license key checked offline, PostgreSQL 15+ with pgvector, and an embeddings endpoint. One inconsistency: the licensing page says a license can be Team, Advanced or Enterprise, while pricing says self-host is Enterprise. | https://mergeharbor.com/deployment ; https://mergeharbor.com/docs/self-hosting ; https://mergeharbor.com/docs/self-hosting-licensing ; https://mergeharbor.com/pricing |
| Eleven git hosts GA from Nov 1 2026 | TRUE by count. The 2026-11-01 entry makes Bitbucket Cloud, Azure DevOps Services and Server, Gitea, Forgejo and Perforce (via Helix Swarm) GA, "alongside" GitHub, GHES, GitLab, GitLab Self-Managed and Bitbucket Data Center. That is 11. In the October GA entry the first six were "in beta". GA does not mean equal depth: approvals evidence works only on GitHub and GitLab, and the proof-run check is documented for GitHub. | https://mergeharbor.com/changelog ; https://mergeharbor.com/compliance ; https://mergeharbor.com/docs/proof-runs |
| CLAUDE.md, AGENTS.md, .cursorrules "read from the merge base" | TRUE. "`CLAUDE.md`, `AGENTS.md`, `.cursorrules`, `.github/copilot-instructions.md`, and `.cursor/rules/*` are read from the merge base and treated as team guidance." Also: "Review configuration is read from the pull request's merge base, so a pull request cannot change how it is reviewed." | https://mergeharbor.com/docs/configuration ; https://mergeharbor.com/security |
| Plugins for Claude Code, Codex, Cursor; CLI via Homebrew | TRUE. `brew install mergeharbor/tap/mh`; Claude Code via `/plugin marketplace add mergeharbor/integrations`; Codex via an MCP server (`mh mcp`). | https://github.com/mergeharbor/integrations ; https://mergeharbor.com/changelog (2026-10-22 entry) |

## 3. How MergeHarbor proof runs work

All from https://mergeharbor.com/docs/proof-runs and https://mergeharbor.com/features/proof-runs (read 2026-11-21) unless stated.

- **Order.** A normal review comes first. A proof run starts after it.
- **Who writes the tests.** The model. It writes "small, targeted tests" for the review's findings and for "edge cases of the functions the pull request changed".
- **Where they run.** A fresh sandbox per run, removed afterwards. No secrets or code host credentials. No network except public package registries during install. Tests run offline unless an admin allows more. Strict CPU, memory, process, disk and time limits. Output is scanned and redacted for secrets. Self-hosted OpenShift installs need gVisor and a fixed user for it (https://mergeharbor.com/deployment). Self-hosted sandboxes run on a separate runner service (operator guide not public; not verified).
- **Setup.** Auto-detects Go, Node (Vitest, Jest, Mocha, node:test), Python (pytest), Java and Kotlin (Maven, Gradle), Ruby, Rust, .NET. `.mergeharbor/sandbox.yml` sets install, test, `run_one`, services (for example Postgres), env and limits. The file is read from the target branch, so a PR cannot change how its own tests run.
- **Outcomes.** Proven: the test fails because the PR has the problem. The reply shows the failing test, the command and its output, and confidence goes up. Discarded: the test was wrong or gave different results on two runs; never posted. Passed: the behaviour holds; shown in the console and can be added to the PR.
- **Does it run the existing suite?** The docs describe running generated tests. They do not say it runs the repo's existing tests. Not verified.
- **Does it check the test passes on the base branch (red on head, green on base)?** Not stated. The only flake check described is "different results on two runs". Heal and Heimdall both document stronger checks (see sections 4 and 6). Not verified for MergeHarbor.
- **Output to the PR.** GitHub check named "MergeHarbor / proof run", replies under findings, a "Run tests" button, and `@mergeharbor test` on demand. "Add this test to the pull request" opens a small PR into the branch; it never pushes to it.
- **Triggers and limits.** Off by default. An admin sets branches, paths, skip drafts, tests per run and runs per month.
- **Cost.** 2 extra credits per run that uses the sandbox, on top of the review (1 credit Standard, 3 Deep, 10 Max). Extra credits are $0.25 each.
- **Effect on the gate.** A reproduced finding counts as "proven" in the merge gate, and passing generated tests raise a 0 to 100 readiness score.

## 4. What blocks a merge, deployment, price

**What blocks.** https://mergeharbor.com/docs/merge-gate (2026-11-21).
- Verdicts: Ready, Ready with warnings, Blocked, Waiting for review.
- Signals: review confidence (a model score), open findings by severity, proof runs, unmet ticket criteria, changed-line coverage and test gaps, change risk, approvals by people (not bots), team-rule findings, dependency risk (with the Security add-on).
- Presets: Relaxed blocks only critical findings and findings a test proved. Standard also blocks review confidence below 3 of 5. Strict blocks major findings and needs 1 approval. Regulated needs 2 approvals and blocks on coverage, test gaps and high risk.
- Readiness is a weighted average of measured signals, forced into the verdict's range (blocked 0 to 49, warnings 50 to 79, ready 80 to 100). An unmeasured signal never blocks.
- Default mode is advisory: the check never fails. A team must switch to enforce and add the check as required in branch protection. A GitHub warning shows as neutral and still lets the merge happen.
- Override: owners and admins can override one commit with a reason; it is audit-logged and a new push re-checks. Can be turned off.
- Gate checks run again on any change (push, finding dismissed, proof run done, policy change).

**Deployment.** MergeHarbor Cloud on AWS (every plan, 14-day trial, no free plan) or self-hosted and on-prem (Enterprise). https://mergeharbor.com/deployment (2026-11-21). Models: Anthropic API, Amazon Bedrock, or an OpenAI-compatible server (vLLM, Ollama, TGI) (https://mergeharbor.com/docs/self-hosting).

**Price.** https://mergeharbor.com/pricing (2026-11-21), per active developer per month, annual billing in brackets.
- Essentials $25 ($20): 100 credits per developer, pooled. No merge gate, no proof runs.
- Team $55 ($45): 200 credits, Deep tier, merge gate, issue triage, proof runs.
- Advanced $85 ($70): 300 credits, Max tier, deep security scans, compliance evidence, automations.
- Enterprise: custom. SSO, SCIM, self-host, air-gapped, GHES and GitLab self-managed and Bitbucket DC and Azure DevOps Server.
- Add-ons: Security (vulnerability management) $15 ($12), Shipyard coding agent $15 ($12). Credits beyond the allowance $0.25. Free for public repos on GitHub, GitLab, Bitbucket, Gitea, Codeberg. 50% off Team for pre-Series A companies for a year.
- Worked example on the page: 25 developers on Team, 12 PRs each, 2 pushes per PR is $1,375 a month, $4.58 per PR.

## 5. MergeHarbor weaknesses

No user complaints, issues or reviews exist yet (no issues on the integrations repo; no HN, Reddit or X threads found; searches 2026-11-21). So the list below comes from its own docs and from what is missing.

1. No track record. Domain 2026-10-18, 0 stars, 1 VS Code install, no named customers, no founders, no funding found. Sources above.
2. No SOC 2 report, while CodeRabbit and Greptile have SOC 2 Type II by MergeHarbor's own comparison page. https://mergeharbor.com/compare (2026-11-01 check date).
3. Self-described pilot benchmark: in-sample, 40 PRs, no intervals, one run per tier. https://mergeharbor.com/benchmarks
4. Advisory by default; proof runs off by default; `proof_run` rule off by default. A team gets no proof gate until it turns three things on. https://mergeharbor.com/docs/merge-gate ; https://mergeharbor.com/docs/proof-runs
5. The gate reads a model confidence score and a model-written "readiness" number. The pass or fail still rests partly on LLM output. https://mergeharbor.com/docs/merge-gate
6. Proof is additive. A finding the verifier model likes is posted even without a test. Unproven findings are not dropped. https://mergeharbor.com/features/review-agent ; https://mergeharbor.com/docs/proof-runs
7. Self-host is Enterprise (sales call, license key, Postgres with pgvector, embeddings endpoint, optional S3, optional gVisor runner). Not a single binary. https://mergeharbor.com/docs/self-hosting
8. Host GA is uneven: approval evidence only on GitHub and GitLab. https://mergeharbor.com/compliance
9. Sells both a coding agent (Shipyard, which writes code that MergeHarbor then reviews) and the independent review. Its own guide argues the writer should not be its only reviewer. The two are not in conflict if the reviewer is separate, but the same vendor owns both sides. https://mergeharbor.com/features/shipyard ; https://mergeharbor.com/guides/coding-agent-vs-independent-review
10. Credit pricing is hard to forecast (1, 3, 10 credits plus 2 for proof, $0.25 overage), though per-PR and monthly caps exist. https://mergeharbor.com/pricing

## 6. Ito, Heal, edgeverdict

Each name is shared by other things. The ones matching code review, proof or merge gating are below.

### Ito (ito.ai), runtime code review

| Item | Finding | Source and date |
|---|---|---|
| What | A GitHub App that builds a single-use copy of your app per PR, drives the affected flows in a real browser, and posts findings with video, logs and repro steps. | https://www.ito.ai/ ; https://www.ito.ai/docs/how-it-works (2026-11-21) |
| Proof | "Attaches the failing test to anything that breaks" (vendor claim). Re-runs on a pushed fix. Proof is a browser run, not a unit test you keep. | https://www.ito.ai/ |
| Gate | None. The FAQ: "Does Ito merge code? No", but customers use it as a signal for automerge. | https://www.ito.ai/pricing |
| Host | GitHub only per its docs. | https://www.ito.ai/docs/guides/connect-repository.md |
| Deploy | Cloud only; no self-host page found. Not verified. | https://www.ito.ai/ |
| Price | Free for 20 reviews. Pro $40 per seat per month, 50 reviews per seat, $1 per extra review. Enterprise from 25 engineers. Free for approved MIT or Apache projects. | https://www.ito.ai/pricing |
| Speed | 30 minutes to 2 hours per PR (vendor). | https://www.ito.ai/pricing |
| Team | Evan Marshall is co-founder and CTO per a podcast page. Funding not found. | https://testguild.com/podcast/a593-evan/ ; web search 2026-11-21 |
| Launch | Listed on Product Hunt and X posts by two people named Makwana (dates not readable). One listing gives release 2026-08-13 (secondary, not verified). | https://www.producthunt.com/products/ito-ai-code-review-that-runs-code (403 to fetch; seen in search only) ; https://aitoolly.com/product/ito |
| Traction | 1,045 GitHub Marketplace installs, per a secondary review read 2026-11-13. SOC 2 Type II per its site. | https://tech-insider.org/ito-ai-code-review-that-runs-your-code/ (the article is marked sponsored) ; https://www.ito.ai/ |
| Weakness | Long waits, needs sandbox secrets and seed data, reads PR text not tickets, runs only for active team members' PRs. | https://tech-insider.org/ito-ai-code-review-that-runs-your-code/ |

Difference from Heimdall: Ito proves behaviour in a running app (end to end, browser). Heimdall proves with unit-level red-first tests and tool results. No gate and no human-approval binding at Ito.

### Heal (heal.dev, MYIA SAS), whole-product bug hunter

| Item | Finding | Source and date |
|---|---|---|
| What | Runs your product in a sandbox that replicates production and files only defects that a failing test reproduces. It runs on the whole product (scheduled, API, or on demand), not per PR. | https://www.heal.dev/ (2026-11-21) |
| Proof | "A test fails twice, then fails again when heal re-runs it itself." Each defect shows red 2 of 2 before the fix and green 2 of 2 after. What it cannot reproduce, it does not file. | https://www.heal.dev/ |
| Gate | None found. Webhooks on "defect reported" exist (2026-11-02). | https://www.heal.dev/changelog |
| Hosts | GitHub, and GitLab from 2026-11-06. | https://www.heal.dev/changelog |
| Position | "Keep your code reviewer." Complements review; does not replace it. Vendor benchmark (October 2026): 70% of the defects Heal found were not found by code-review agents; compares with CodeRabbit, Greptile, cubic. Home page says 174 PRs. Vendor-run, not verified. | https://www.heal.dev/benchmark |
| Price | Free trial, no card. No price page (the `/pricing` path returns 404). Not found. | https://www.heal.dev/pricing (404) |
| Trust | "SOC 2 certified" with a trust center (page content not readable). Company name MYIA SAS in the footer. | https://www.heal.dev/ ; https://trust.heal.dev |

Difference from Heimdall: Heal is a bug hunter that outputs defects with proof. It is not a merge gate and has no approval record. Its red-twice-green-twice check is stronger than what MergeHarbor documents and worth copying.

### edgeverdict (anp0429/edgeverdict), open source

| Item | Finding | Source and date |
|---|---|---|
| What | "A code review gate that verifies changes by executing tests, not by judging diffs. LLM proposes; a deterministic gate decides." MIT, Python 3.11+, `pip install edgeverdict`. | https://github.com/anp0429/edgeverdict (2026-11-21) |
| Proof | A model proposes edge-case tests for a change. A deterministic harness runs each in a clean checkout, in a hardened network-isolated Docker sandbox by default. A gap is reported only if the test compiles, runs and fails. Verdicts: BROKEN, HELD ("N attempts, none broke it, never certification"), STOPPED. Exit codes 0, 2, 1. | README, same URL |
| Does not | Does not grade diffs. Does not run your existing suite. Reviews pick targets from the diff or a named file. | README |
| Surfaces | CLI, GitHub Action PR comments, MCP server for Claude Code. `review` mode is always advisory in exit codes. | README |
| Activity | Created 2026-07-13, last push 2026-10-13, 2 stars, 1 fork. PyPI release 0.10.0 (earlier 0.9.0 on 2026-08-07). Renamed from agentboard and reviewgate. The solo account has 6 public repos. | `gh repo view anp0429/edgeverdict` ; https://pypi.org/project/edgeverdict/ (2026-11-21) |
| Weakness | Tiny, one maintainer, function-scoped, no merge-base policy, no human approval, no audit trail. One search summary said generated tests may reach the network and a live database; the README says network is off by default. I did not read SECURITY.md. Not verified. | https://github.com/anp0429/edgeverdict |

Difference from Heimdall: the closest idea to Heimdall's rule "a model proposes, a deterministic gate decides, no model decides". It is a library, not a product with a gate, memory or approval.

## 7. Other 2026 products that sell proof or evidence, which research 01 missed

| Product | What | Source and date |
|---|---|---|
| Greptile TREX | "Test Run EXecute". Runs the PR branch in a sandbox, starts services, mocks inputs, clicks through UI, leaves logs, screenshots and traces on the PR. Launched 2026-06-15 (search summary); claims about 20% more bugs (vendor claim); reported extra $2 per run (not verified on the page). Research 01 had only a secondary claim. | https://www.greptile.com/trex.md (read 2026-11-21) ; https://www.greptile.com/blog/trex (seen in search only) |
| mergeproof (ahmed-3m) | "Let AI agents merge only with proof": a zero-dependency, LLM-free gate. A policy file declares what a change must prove; CI enforces it. MIT, 1 star, created 2026-10-22, pushed 2026-11-18. Closest to Heimdall's no-model gate, but it checks declared evidence, it does not produce proof. | https://github.com/ahmed-3m/mergeproof ; https://pypi.org/project/mergeproof/ (2026-11-21) |
| Imbue Vet | Open-source review that checks a diff against intent. Does not run tests. A reported bug (issue 225) where agentic mode can print "no issues" with exit 0. Not a proof tool; listed so it is not mistaken for one. | https://github.com/imbue-ai/vet ; https://github.com/imbue-ai/vet/issues/225 (seen in search 2026-11-21) |
| Noise | Several GitHub PRs and DEV posts describe home-made "merge gates" (for example `CP-MERGE-GATE`, `agentic-ayudha`, `codeframe`). These are personal projects. I did not read them. | Search results, 2026-11-21. Not verified |

No other product selling the exact phrases "proven findings" or "a test per finding" turned up in searches on 2026-11-21. Hacker News and Product Hunt could not be searched directly. A direct HN search is still open.

## 8. Heimdall against MergeHarbor

Heimdall facts come from `STRATEGY.md` (draft 2, 2026-11-21), which states the engine is built and the shell is a demo. No measurement of Heimdall's speed, cost or proof rate exists yet.

| Area | MergeHarbor (docs) | Heimdall | Verdict |
|---|---|---|---|
| Failing test as proof of a finding | Yes, model-written, sandbox, discards flaky | Yes (E1), red-first | Same |
| Proof from non-test sources | Not described | E2 tool result, E3 team-memory fact | Heimdall wider (design; E3 not proven in use) |
| Unproven findings | Posted if the verifier confirms; proof raises confidence | Dropped and counted | **Different** |
| Proves other reviewers' findings | Not described. NOT FOUND | Planned: ingest CodeRabbit, Copilot and Bugbot findings and prove or drop each | **Different** (planned, not built) |
| Merge gate | Yes; model confidence and readiness score feed it; advisory by default | Pure-Go gate, no model decides; fast required check plus slow proof run that may say `unknown` | Same idea, different trust rule |
| "Proven findings block" | Yes | Yes by design | Same |
| Human approval bound to the exact commit | Records which commit was approved; invalidation on a new push is not stated for its own gate (it re-checks on every push). Not verified | Core rule: approval on this SHA, void on any new push | Heimdall clearer; GitHub offers it natively too |
| Policy and rule files from merge base | Yes, config, `sandbox.yml`, rule files | Yes | **Same. Not a difference** |
| Break-glass / override with reason, audited | Yes | Yes | Same |
| Shadow mode first | Yes (advisory default) | Yes (two weeks) | Same |
| Memory from the developer's agent sessions | Learning from review feedback with approval; reads rule files | Local CLI, mines sessions, signed rule cards, enforced at merge | Heimdall different; MergeHarbor has nothing like session mining (NOT FOUND) |
| Proof jobs (upgrade PRs with proof) | Shipyard coding agent (plan, sandbox, tests, PR); fix PRs for vulnerabilities | Upgrade, repro and test jobs with proof | Same space; MergeHarbor ahead and shipped |
| Self-host | Enterprise, license, Postgres plus pgvector, K8s or Compose | Customer runner (Action) or one VM; 42 MiB server | Heimdall lighter; MergeHarbor more complete |
| Git hosts | 11 | GitHub (Gitea and others planned) | Behind |
| Languages for proof | 8 stacks detected | Go runner only | Behind |
| Compliance evidence | Sealed hash-chained ledger, starter packs, auditor reports | Upsell, Vanta or Drata export, not built | Behind |
| Enterprise auth | SAML, OIDC, SCIM, roles, API keys, IP allowlist | Board has no auth | Behind |
| Developer surfaces | VS Code, CLI, MCP, Claude Code and Codex plugins, chat apps | Chat via Switch; memory CLI planned | Behind |
| Published benchmark | Pilot, in-sample, 40 PRs | None | Behind (theirs is weak) |
| Price | $45 to $70 per developer annual, credits | Proposed $15 Team, $35 Regulated | Heimdall cheaper (proposal only) |
| Free tier | None (14-day trial; free for open source) | Free memory CLI and shadow mode | Heimdall different |
| Track record | None found | Demo, one live run | Both unproven |

## 9. Heimdall's sharpest defensible difference

Candidates, strongest first.

1. **No proof, no comment, with the count shown, and applied to any reviewer's findings.** A MergeHarbor finding can reach the PR with only a second model's agreement. Its proof run is optional and adds confidence. Heimdall's one sentence already says the opposite: a finding without proof is dropped and counted. If Heimdall also takes CodeRabbit, Copilot and Bugbot comments and proves or drops each, it sells something none of the products above document: "your existing review bots, filtered to what a machine can prove". It does not compete with MergeHarbor on review quality. It sits on top of any reviewer. Risk: MergeHarbor, Greptile or Ito could add the same rule in a release, since proof runs already exist. The advantage lasts only while it is a policy they do not want (it lowers their comment count and recall numbers).
2. **No model in the gate.** MergeHarbor's verdict uses a model confidence score and a weighted readiness number. Heimdall's gate is plain Go over evidence classes. This is a real difference for buyers who must explain a block. It is hard to market: MergeHarbor's rules are also deterministic once the inputs exist.
3. **Light self-hosting without an enterprise contract.** MergeHarbor self-host is sales-gated and heavy. A single binary on the customer's runner or one VM, with no license call and shadow mode free, serves teams that cannot send code out and will not buy Enterprise. This is a cost and ops advantage, not a capability. Cheap to copy if MergeHarbor ships a lighter bundle (Compose already exists).
4. **Memory as a free open-source door.** MergeHarbor has no session-mining memory (NOT FOUND in its docs). Mining is not a moat (STRATEGY section 5 says so), but memory enforced at merge with E3 evidence is a different product shape.

Not a difference, so stop claiming it: reading policy and rule files from the merge base (MergeHarbor, https://mergeharbor.com/docs/configuration), proven findings block, a sandbox with no secrets, break-glass, advisory mode, self-hosting, audit records. STRATEGY section 5 lists "Rules from the merge base" as a reason to win. Delete or rewrite that line. Also section 5 and research 01 section 6 say "no tool refuses to post a blocking finding without proof". After this note, rewrite it as: "no tool documents dropping unproven findings and counting them". MergeHarbor's Relaxed preset does block on proven findings.

## 10. What to do with this

1. Fix claims in `STRATEGY.md` and `research/01 §6`: merge-base rules are not unique; "nobody ties blocking to proof" is now false for MergeHarbor (Relaxed preset) and Greptile TREX is primary-sourced.
2. Build the demo around the difference: run a rival reviewer's output through Heimdall, show "40 in, 4 proven, 36 dropped, with cost". This is the one thing MergeHarbor does not show.
3. Copy Heal's red-twice then green-after-fix check into E1 if it is not already there. Document the base-branch check explicitly, since MergeHarbor does not.
4. Do not spend effort on host count, SSO or compliance depth before the first design partner. MergeHarbor is far ahead there and it is not the wedge.
5. Re-check MergeHarbor in two weeks: stars, installs, a first customer, a founder post, any HN thread, SOC 2 progress. Today's evidence is a month of activity.

## Sources

MergeHarbor (all read 2026-11-21): https://mergeharbor.com/ ; https://mergeharbor.com/changelog ; https://mergeharbor.com/pricing ; https://mergeharbor.com/docs/proof-runs ; https://mergeharbor.com/features/proof-runs ; https://mergeharbor.com/docs/merge-gate ; https://mergeharbor.com/docs/configuration ; https://mergeharbor.com/compliance ; https://mergeharbor.com/security ; https://mergeharbor.com/deployment ; https://mergeharbor.com/docs/self-hosting ; https://mergeharbor.com/docs/self-hosting-licensing ; https://mergeharbor.com/benchmarks ; https://mergeharbor.com/compare ; https://mergeharbor.com/about ; https://mergeharbor.com/blog ; https://mergeharbor.com/features/review-agent ; https://mergeharbor.com/features/shipyard ; https://mergeharbor.com/guides/coding-agent-vs-independent-review ; https://mergeharbor.com/docs/ai-data ; https://mergeharbor.com/sitemap-0.xml
GitHub (`gh`, read only): https://github.com/mergeharbor/integrations ; https://github.com/mergeharbor/mh ; https://github.com/tastaslim/mergeharbor-qa-demo ; https://marketplace.visualstudio.com/items?itemName=mergeharbor.mergeharbor
Ito: https://www.ito.ai/ ; https://www.ito.ai/pricing ; https://www.ito.ai/docs/how-it-works ; https://www.ito.ai/docs/llms.txt ; https://tech-insider.org/ito-ai-code-review-that-runs-your-code/ ; https://testguild.com/podcast/a593-evan/ ; https://aitoolly.com/product/ito
Heal: https://www.heal.dev/ ; https://www.heal.dev/benchmark ; https://www.heal.dev/changelog
edgeverdict: https://github.com/anp0429/edgeverdict ; https://pypi.org/project/edgeverdict/
Others: https://www.greptile.com/trex.md ; https://github.com/ahmed-3m/mergeproof ; https://pypi.org/project/mergeproof/ ; https://github.com/imbue-ai/vet

Not verified: MergeHarbor founders, funding, customers and any launch post; whether proof runs run the existing suite or check the base branch; the MergeHarbor sandbox runner internals; Ito funding and launch date; Heal pricing and its benchmark method; Greptile TREX price; Hacker News and X search (the search tool returned no direct hits; X posts about Ito were seen only as search snippets).
