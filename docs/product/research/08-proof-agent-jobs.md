# 08. Proof agent jobs: which machine-checkable jobs should Heimdall sell?

Date of research: 2026-11-21. Method: about 57 web searches (standard and extended), 5 primary pages read in full, and the
hn.algolia.com API for Hacker News comments. Reddit was blocked and not tried. X was out of scope.

Source rules for this file:
- Every number has a URL and a date. "Accessed 2026-11-21" means the page shows no date I could read.
- "Secondary" means I read the number in a search summary or a third-party page, not on the vendor's own page.
- "Vendor" means the number comes from a company that sells the thing it measures.
- "Not verified" means I found no source I trust for it.

---

## 0. What Heimdall already has (8 lines)

1. An event-sourced Go server with a reconciler and an outbox. It replays state and survives restarts (`STRATEGY.md §3`).
2. A rootless Podman sandbox with no network and no capabilities. Only a Go toolchain profile ships today.
3. A Test Agent that writes tests from the ticket and must see them fail first. A test that passes early is rejected.
4. Evidence tiers: E1 a failing test, E2 a deterministic tool result, E3 a confirmed team fact. E4 reasoning never blocks.
5. A plain-Go gate of ten rules. No model sits in the decision path.
6. Human approval bound to one exact commit. A new push voids it, and the record is hash-chained.
7. Chat rooms (Switch bridged to Slack, Discord, Mattermost, Telegram) as the control room.
8. Cheap models (DeepSeek V4 Flash via an OpenAI-compatible port), all on one company VM (2 vCPU, 16 GB reference).

Gaps that matter for this research: the sandbox has no network, so any job that installs packages needs a registry
mirror or a pre-fetch step. Only Go repos run today. The install takes "an afternoon" (`STRATEGY.md §3`).

---

## 1. The thesis under test

"Heimdall should be the company's own agents, on its own VM, that only hand over work with proof. The sellable unit is
an agent job whose outcome a machine can check, so cheap models plus a sandbox plus the evidence rule do it well."

I tried to break it on four points:
1. Do the most checkable jobs have real pain and budget?
2. Has someone with distribution already shipped the same loop in 2026?
3. Can cheap models do the job at a cost below the price?
4. Does "machine-checkable" mean "worth paying a new vendor for", or does it mean "easy for everyone to build"?

---

## 2. The jobs, one by one

### Job 1. Fix broken dependency, Dependabot, Renovate and CVE upgrade PRs until the build is green

**Pain evidence**

| Fact | Source |
|---|---|
| Only 65.42% of Dependabot security PRs were merged in 2,904 JS projects. 34.58% were not | [MSR 2021 paper](https://realiselab.github.io/publications/On-the-Use-of-Dependabot-Security-Pull-Requests.pdf), 2021 |
| Bot PRs in general: 13.2% merged, 37.6% deleted, 16.4% stale | [LinearB bot report](https://assets.linearb.io/image/upload/v1727274292/Bot_Automation_Report.pdf), 2024, vendor, secondary |
| The median dependency trails its latest major version by 278 days, up from 215 the year before | [Datadog State of DevSecOps 2026 via Help Net Security](https://www.helpnetsecurity.com/2026/03/02/devsecops-supply-chain-risk-security-debt/), 2026-03-02 |
| 87% of organizations run at least one exploitable vulnerability in production, in 40% of their services | same, 2026-03-02 |
| Only 18% of critical dependency vulnerabilities stay critical after runtime context | same, 2026-03-02 |
| 85% of open source findings were still open at the study cutoff | [Black Duck release](https://www.prnewswire.com/news-releases/newly-discovered-software-vulnerabilities-outpace-fixes-18-to-1-as-exploit-windows-shrink-new-black-duck-research-shows-302941220.html), 2026, vendor, secondary |
| Third-party SCA flaws have a 358-day remediation half-life (Veracode) | [Red Hat Q3 2026 signals](https://www.redhat.com/en/blog/open-source-security-signals-q3-2026), 2026, secondary |
| "A dependency upgrade for critical infrastructure might contain breaking changes that take months to fix" | [HN, lucideer](https://news.ycombinator.com/item?id=49154797), 2026-08-03 |
| A team built an "agentic merge queue" just for Dependabot PRs | [HN, jcarver](https://news.ycombinator.com/item?id=50477957), 2026-11-13 |

Hours per team spent on dependency updates: **not verified**. I found no survey that isolates it. The closest is "seven
in 10 organizations spend over 6 hours per week on patching" ([Canonical and IDC](https://canonical.com/blog/70-of-it-teams-spend-more-than-6-hours-per-week-on-security-patching-new-idc-research), date not verified), which covers OS patching too.

**Who sells it, at what price, self-hosted or not**

| Vendor | What | Price | Self-hosted? |
|---|---|---|---|
| GitHub | Assign a Dependabot alert to Copilot, Claude or Codex; the agent opens a draft PR and tries to fix test failures | Needs Code Security ($30 per active committer per month) plus a Copilot plan with coding agent | github.com only. [Changelog 2026-04-07](https://github.blog/changelog/2026-04-07-dependabot-alerts-are-now-assignable-to-ai-agents-for-remediation/), [Code Security price 2025-03-04](https://github.blog/changelog/2025-03-04-introducing-github-secret-protection-and-github-code-security/) |
| Snyk | Remediation Agent with breaking-change assessment; Agent Fix for SAST | Team from $25 per developer per month, Enterprise by quote | SaaS. [Stork summary](https://www.stork.ai/en/snyk-ai), accessed 2026-11-21, secondary |
| FOSSA fossabot | AI agent for upgrades, adapts app code to breaking changes, JS/TS first | $15 free credit per month at launch; paid add-on by quote | SaaS. [Launch post](https://fossa.com/blog/fossabot-dependency-upgrade-ai-agent/), Oct 2025 |
| Socket Fix | Upgrades vulnerable deps, test mode keeps the bump only if tests pass | Team $25 per developer per month | CLI plus SaaS. [Socket pricing](https://socket.dev/pricing), accessed 2026-11-21; [Socket Fix](https://socket.dev/blog/introducing-socket-fix) |
| Endor Labs | Upgrade impact analysis predicts breaking changes (not JS or Rust) | Usage-based, by quote | SaaS. [Docs](https://docs.endorlabs.com/upgrades-and-remediation/upgrade-impact-analysis), accessed 2026-11-21 |
| Mend Renovate | Opens update PRs, Merge Confidence, automerge. No AI repair found | Community free; Enterprise by quote | Yes, self-hosted on GHES, GitLab Self-Managed, Bitbucket DC. [Mend docs](https://docs.mend.io/renovate/latest/mend-renovate-enterprise-self-hosted) |
| Gitar (Sonar) | Fixes failing CI on any PR until green | $20 to $40 per user per month; Enterprise flat fee per PR | SaaS. [Sonar compare](https://www.sonarsource.com/products/gitar/compare/), accessed 2026-11-21 |

**Verifiability.** Very high. Snyk's own benchmark defines a pass as "the vulnerabilities are gone and the project still
installs, builds, and passes its tests" ([VulnBump](https://snyk.io/blog/vulnbump/), Oct 2026). That is E2 (scanner
before and after) plus E1 (build and tests). The weak spot is test tampering. Agents edit tests to go green
([SpecBench](https://arxiv.org/pdf/2605.21384v1), May 2026; [EvilGenie](https://arxiv.org/pdf/2511.21654), Nov 2025;
"preexisting flaky tests, not related" [HN, Muromec](https://news.ycombinator.com/item?id=49973137), 2026-10-06).
Heimdall's protected test files and gate catch exactly this.

**Cheap models.** Mixed but workable.
- A no-AI baseline that only applies the recommended version passed 46.9% of 307 VulnBump tasks
  ([Snyk](https://snyk.io/blog/vulnbump/), Oct 2026, vendor). So about half the work needs no model at all.
- On real breaking Java updates (BUMP), the best model reached a 27% build success rate
  ([Byam, arXiv 2505.07522](https://arxiv.org/html/2505.07522v3), 2025). Hard breaks stay hard.
- DeepSeek V4 Flash: 79.0% SWE-bench Verified claimed, 73.7% in DeepSeek's report; $0.14 input and $0.28 output per
  million tokens at launch ([stackfutures](https://stackfutures.com/blog/deepseek-v4-flash-launch-moe-pricing/), Apr 2026, secondary).
- A failed attempt costs cents and is thrown away. Only a green, proven PR reaches a person.

**Willingness to pay.** Strong. Buyers already pay $25 to $30 per developer per month for SCA and autofix. Companies pay
a median $30,750 a year to HeroDevs just to stay on end-of-life frameworks instead of upgrading
([Vendr](https://www.vendr.com/marketplace/herodevs), accessed 2026-11-21, 32 purchases).

**Fit with Heimdall.** Good, with one real gap: package install needs network. Fix: a deterministic resolver step with
network fetches into a local cache or the customer's Artifactory/Nexus, then the agent runs offline. Multi-language
profiles are needed (npm, Maven, pip). Only Go exists today.

**Scores:** pain 4, pay 4, verifiability 5, fit 4, competition 2 (GitHub, Snyk, FOSSA, Socket ship it in SaaS). In the
self-hosted git segment (GitLab Self-Managed, GHES, Bitbucket DC, air-gapped) competition is 4: Renovate self-hosts but
does not repair, and GitHub's agent assignment is github.com only.

---

### Job 2. CI failure triage and flaky test detection, quarantine and fix

**Pain evidence**

| Fact | Source |
|---|---|
| About 15% of Jira backend failures come from flaky tests; reruns waste over 150,000 developer hours a year | [Atlassian engineering](https://www.atlassian.com/blog/atlassian-engineering/taming-test-flakiness-how-we-built-a-scalable-tool-to-detect-and-manage-flaky-tests), date not verified |
| At least 2.5% of total developer time goes to flaky tests (5 years of CI data, one company) | [TUM/CQSE, ICST 2024](https://mediatum.ub.tum.de/doc/1730194/1730194.pdf), 2024 |
| Google: about 1.5% of all test runs report a flaky result | [Google Testing Blog, Micco](https://testing.googleblog.com/search/label/John%20Micco), 2016 |
| Google: 16% of tests show some flakiness; 84% of pass-to-fail transitions involve a flaky test | [Survey, arXiv 2203.00483](https://arxiv.org/pdf/2203.00483), 2022, secondary citation |
| In GitHub Actions, about 1 in 4 failed jobs is flaky (80 projects) | [arXiv 2602.02307](https://arxiv.org/html/2602.02307v1), Feb 2026 |

**Who sells it**

| Vendor | What | Price | Self-hosted? |
|---|---|---|---|
| Datadog Test Optimization + Bits | Detect, quarantine, AI root-cause category; Bits drafts fix PRs for high-confidence flakes | About $20 per committer per month annual, $29 on demand | SaaS. [Datadog price list](https://www.datadoghq.com/pricing/list/), page age not verified; [Bits post](https://www.datadoghq.com/blog/bits-ai-test-optimization/) |
| Trunk Flaky Tests | Detect and quarantine. Built a fix agent, then stepped back | Free up to 5 committers; Team price not published | SaaS. [Trunk pricing](https://trunk.io/pricing), accessed 2026-11-21 |
| CircleCI Chunk | Agent that fixes flaky tests, BYO Anthropic or OpenAI key | Free in beta, paid at GA | CircleCI only. [Docs](https://circleci.com/docs/guides/test/fix-flaky-tests), accessed 2026-11-21 |
| Bitbucket Tests | Fix Flaky Test AI agent opens a PR | Rovo credits, not verified | Cloud. [Atlassian community](https://community.atlassian.com/forums/Bitbucket-articles/Bitbucket-Tests-Introducing-the-Fix-Flaky-test-AI-Agent/ba-p/3228395), 2026-04-29 |
| GitHub Copilot | "Fix failing checks" from a failed Actions run or the merge box | Copilot AI credits | Cloud, or ARC self-hosted runners. [Changelog](https://github.blog/changelog/2026-07-23-github-mobile-fix-failing-actions-checks-with-copilot-cloud-agent/), 2026-07-23 |
| GitLab Duo | Root-cause a failed pipeline and open a fix MR | Credits, Premium/Ultimate by quote | Duo Self-Hosted exists. [GitLab](https://about.gitlab.com/gitlab-duo-agent-platform/), accessed 2026-11-21 |
| Gitar (Sonar) | Fix CI until green, then commit | $20 to $40 per user | SaaS. [Sonar](https://www.sonarsource.com/products/gitar/compare/) |
| BuildPulse | Flaky detection | $99 to $999 per month | SaaS. [fitgap](https://us.fitgap.com/products/029723/buildpulse), secondary, not verified |
| Mergify | CI Insights, auto-rerun to catch new flakes | Free up to 5 contributors, then custom | SaaS. [Mergify pricing](https://mergify.com/pricing), accessed 2026-11-21 |

**The warning sign.** Trunk built "a monolithic agent that would review your flaky tests and open PRs." It got "tunnel
vision," and partners said "these proposed fixes don't follow our repo's conventions." Trunk concluded: "Our moat wasn't
a coding agent that fixes flaky tests; it was the data and insights to do so"
([Trunk blog](https://trunk.io/blog/don-t-build-agents-build-context-enrichment), 2026-02-11). A Trunk docs PR then
deprecated AI investigations ([trunk-io/docs2 #478](https://github.com/trunk-io/docs2/pull/478), 2026-11-11).

**Verifiability.** Medium. "N reruns green" is statistical. A flake at 1% needs hundreds of runs to rule out. Kong fixed
12 of its 15 flakiest tests in 1.5 weeks, at about 4 to 5 hours per test, "mostly in verification"
([stack-archive summary](https://stack-archive.com/blog/kong-gateway-claude-code-flaky-tests-2026/), 2026, secondary;
primary [Kong post](https://konghq.com/blog/engineering/how-we-used-agentic-ai-to-fix-kong-gateways-flakiest-tests) not opened).
Two "flakes" were real bugs. That is a good E1 story, but slow.

**Cheap models.** Fair for timeouts and ordering bugs. Weak for flakes that need the real environment (services,
network, timing on shared runners). A no-network sandbox cannot reproduce network flakes by design.

**Scores:** pain 4, pay 3, verifiability 3, fit 3, competition 1 (every CI and observability vendor shipped it in 2026).

---

### Job 3. Missing-test backfill and characterization tests on legacy code

**Pain evidence**

| Fact | Source |
|---|---|
| Developers spend about 20% of their time writing unit tests; 48% struggled to meet coverage targets | [Diffblue 2019 survey](https://www.diffblue.com/resources/2019-diffblue-developer-survey/), 2019, vendor |
| Goldman Sachs raised one legacy module from 36% to 72% coverage; 3,211 tests generated | [Diffblue case](https://diffblue.com/resources/how-did-goldman-sachs-double-the-code-coverage-for-one-of-their-legacy-applications-overnight), date not verified, vendor |
| Developers feel they lack time for testing (284 respondents) | [Straubinger and Fraser, arXiv 2309.01154](https://arxiv.org/pdf/2309.01154), 2023 |
| Upgrades need tests first: "It's faster to rewrite legacy code that has unit tests" (75% agree) | [Diffblue 2019 survey](https://www.diffblue.com/resources/2019-diffblue-developer-survey/), 2019, vendor |

**Who sells it**

| Vendor | Price | Self-hosted? |
|---|---|---|
| Diffblue Cover (Java, now Python) | Developer $30 per month or $330 per year, 100 tests per month; Teams about $30k per year (aggregator, not verified) | Runs locally and in CI. [Diffblue](https://www.diffblue.com/resources/diffblue-cover-is-now-generally-available-to-individual-developers/), about 2024 |
| Qodo Cover | Open source (AGPL listed), still labeled Alpha | Yes. [GitHub](https://github.com/qodo-ai/qodo-cover), accessed 2026-11-21 |
| General agents (Copilot, Claude Code, Cursor) | Bundled | Mixed |

**Verifiability.** High if the proof is a mutation score, not coverage. One study shows LLM tests with 100% line and
branch coverage that killed only 4% of mutants ([MutGen, arXiv 2506.02954](https://arxiv.org/html/2506.02954v6), 2025).
Meta's mutation-guided ACH got 73% of its tests accepted by engineers, 140 of 191
([arXiv 2501.12862](https://arxiv.org/pdf/2501.12862), Jan 2025). Proof rule: each new test must kill a mutant the old
suite missed. That is Heimdall's red-first rule pointed at a mutant instead of a missing feature.

The catch: a characterization test locks in current behavior, bugs included. That is fine for "safe to refactor" and
not fine as "correct."

**Cheap models.** Good. Writing a test against a known mutant is a small, local task, and a mutant that survives is a
cheap, deterministic retry signal.

**Willingness to pay.** Medium. Banks pay (Goldman, Citi listed as Diffblue customers, secondary). Most teams treat
tests as a cost center. Diffblue had "fewer than a dozen paying customers" at one point
([TechTarget](https://www.techtarget.com/it-infrastructure/news/252527416/Diffblue-expands-AI-powered-unit-testing-platform), date not verified).

**Scores:** pain 3, pay 3, verifiability 4, fit 5, competition 3.

---

### Job 4. Framework and language version migrations

**Pain evidence**

| Fact | Source |
|---|---|
| Airbnb moved about 3,500 test files from Enzyme to RTL in 6 weeks, against a 1.5 engineer-year estimate; 97% automatic | [Airbnb tech blog](https://airbnb.tech/?p=366), Mar 2025; [InfoQ](https://www.infoq.com/news/2025/03/airbnb-llm-test-migration/) |
| Google int32 to int64: 80% of landed changes AI-authored, about 50% time saved (self-reported) | [Google Research](https://research.google/blog/accelerating-code-migrations-with-ai/), 2024; [arXiv 2501.06972](https://arxiv.org/html/2501.06972v1), Jan 2025 |
| Amazon: 30,000 apps moved to Java 17, "4,500 developer-years" and $260M a year saved | [AWS blog](https://aws.amazon.com/blogs/devops/amazon-q-developer-just-reached-a-260-million-dollar-milestone), Aug 2024, vendor |
| Java 8 still in about 23% of surveyed use | [DevClass on State of Java](https://www.devclass.com/development/2025/01/30/state-of-java-report-shows-strong-migration-from-java-8-rise-of-apache-spark/1625021), 2025-01-30 |
| One Rails jump (3.2 to 4.2) cost $100k to $250k at a consultancy | [FastRuby](https://rubyweekly.com/link/153117/web), older post, date not verified |

**Who sells it**

| Vendor | What | Price | Self-hosted? |
|---|---|---|---|
| AWS Transform custom | Agentic transformations | $0.035 per agent-minute; a 17,000-line Java upgrade took about 72 minutes, $2.52 | AWS only. [Devoteam](https://www.devoteam.com/expert-view/aws-transform-and-agentic-modernisation/), 2026, secondary; [Qovery](https://www.qovery.com/blog/aws-transform-what-it-does-and-what-it-costs), 2026 |
| Moderne (OpenRewrite) | Deterministic recipes plus agent Moddy | Typical $250k a year Commercial (third-party, not verified); raised $30M Series B | Private SaaS, on-prem, air-gapped. [TechCrunch](https://techcrunch.com/2025/02/11/moderne-raises-30m-to-solve-technical-debt-across-complex-codebases/), 2025-02-11; [ace8](https://www.ace8.io/moderne-vs-openrewrite/), not verified |
| Sourcegraph Agentic Batch Changes | Large-scale changes across repos | Pay only per merged changeset; rates in Enterprise Portal | Self-hosted from Sourcegraph 8.0. [Pricing blog](https://sourcegraph.com/blog/agentic-batch-changes-pricing), 2026-09-16 |
| Grit | Sunset after Honeycomb bought it; GritQL stays open | n/a | [Honeycomb](https://www.honeycomb.io/blog/honeycomb-acquires-grit), Apr 2025 |
| Codemod | Codemod platform | Sources conflict ($10 Pro vs $1,000 Team) | Not verified |

**Verifiability.** Medium. "Build and tests green" proves only what the tests cover. Moderne pairs with Diffblue for this
reason ([SiliconANGLE](https://siliconangle.com/?p=696123), 2025-05-13). So job 4 depends on job 3.

**Cheap models.** Good for repetitive per-file edits with a recipe. Poor for cross-cutting framework changes.

**Scores:** pain 4, pay 5 (big deals), verifiability 3, fit 2 (needs many toolchains and long jobs), competition 2.
AWS sets a price floor of a few dollars per upgrade.

---

### Job 5. Evidence-backed PR review and merge gate (the current product)

Pain and white space are in `research/01 §6` and `research/02` (rank 1 pain: review cannot keep up). New 2026 points:
- 61.4% of AI-generated PRs get no human review, per an EASE 2026 study cited by Mend
  ([Mend blog](https://www.mend.io/blog/mend-renovate-enterprise-agentic-scale/), 2026-09-03, secondary).
- Review prices are falling to commodity: GitLab Code Review Flow is a flat $0.25 per review
  ([GitLab](https://about.gitlab.com/blog/agentic-code-reviews-with-flat-rate-pricing), 2026-03-19). Copilot code review
  now also consumes Actions minutes ([changelog](https://github.blog/changelog/2026-04-27-github-copilot-code-review-will-start-consuming-github-actions-minutes-on-june-1-2026/), 2026-04-27).
  Sentry is removing Seer Code Review on 2026-12-08 ([Gleap summary](https://www.gleap.ai/blog/sentry-pricing), secondary).

**Verifiability.** A blocking finding with E1 proof is checkable. "No bugs found" is not. Buyers pay for the first and
get the second by default.

**Scores:** pain 5, pay 3, verifiability 3, fit 5, competition 2.

---

### Job 6. Other jobs with checkable outcomes

| Job | Evidence | Who sells, price | Scores (pain, pay, verif, fit, open) |
|---|---|---|---|
| **6a. Bug or incident to red repro test, then fix** | Sentry Seer finds root cause and opens PRs. It is $40 per active contributor per month, moving to $1 per issue fix run on 2026-12-08, and works only with GitHub.com and GitLab.com, not self-hosted ([Gleap](https://www.gleap.ai/blog/sentry-pricing), secondary). Proof is exactly E1: a test red on the bug commit, green on the fix | Sentry Seer; general coding agents | 4, 3, 5, 5, 3 |
| **6b. SAST finding autofix** | Copilot Autofix: median 28 minutes vs 1.5 hours manual ([GitHub](https://github.blog/news-insights/product-news/secure-code-more-than-three-times-faster-with-copilot-autofix/), Aug 2024, vendor). Over 100,000 fixes applied in 6 months ([HN, GitHub PM](https://news.ycombinator.com/item?id=45500616), 2025-10-07). Pixee claims a 76% merge rate ([BusinessWire](https://www.businesswire.com/news/home/20250522057559/en), 2025-05-22, vendor) | Copilot Autofix inside $30 Code Security; Snyk Agent Fix; Pixee priced by findings; Mobb $20 to $40 per dev (secondary); GitLab auto-resolve | 3, 4, 4, 3, 1 |
| **6c. Lint and type-debt burn-down** | Example: mypy strict showed 5,591 errors over 312 files in one repo ([GitHub issue](https://github.com/evanwtf/local-llm/issues/947), 2026). No buyer evidence found | Nobody sells it as a product, found | 2, 1, 5, 4, 5 |
| **6d. Proof gate for other agents' PRs** (Copilot, Dependabot, Devin output) | Dependabot PRs now come from agents (row 1). Teams build their own "Janitor" for overnight Dependabot merges ([ZenBusiness](https://tech.zenbusiness.com/toss-the-keys-dont-watch-the-catch-how-we-let-ai-merge-dependabot-prs-overnight/), 2026) | patchwave (not verified), Mergify queues, DIY | 4, 3, 4, 5, 3 |

---

## 3. Score table

Scale 1 to 5. Competition: 5 means open. Total is the plain sum, out of 25. These are my judgments from the evidence above.

| # | Job | Pain | Pay | Verif. | Fit | Open | Total |
|---|---|---|---|---|---|---|---|
| 1 | Dependency and CVE upgrade fixes (all markets) | 4 | 4 | 5 | 4 | 2 | 19 |
| 1s | Same, **self-hosted git and regulated only** | 4 | 4 | 5 | 4 | 4 | **21** |
| 6a | Bug to red repro test to fix | 4 | 3 | 5 | 5 | 3 | 20 |
| 6d | Proof gate for agent and bot PRs | 4 | 3 | 4 | 5 | 3 | 19 |
| 5 | PR review and merge gate | 5 | 3 | 3 | 5 | 2 | 18 |
| 3 | Characterization tests, mutation-gated | 3 | 3 | 4 | 5 | 3 | 18 |
| 6c | Lint and type debt | 2 | 1 | 5 | 4 | 5 | 17 |
| 4 | Framework migrations | 4 | 5 | 3 | 2 | 2 | 16 |
| 6b | SAST autofix | 3 | 4 | 4 | 3 | 1 | 15 |
| 2 | CI triage and flaky tests | 4 | 3 | 3 | 3 | 1 | 14 |

---

## 4. Other products on the same assets (checked and rejected)

| Idea | Why not |
|---|---|
| Sell the sandbox runtime | E2B and Daytona both charge about $0.0504 per vCPU-hour and offer self-host paths ([Fly.io comparison](https://fly.io/learn/ai-sandbox-pricing/), 2026, secondary). A commodity with funded leaders |
| Self-hosted background agent platform | Crowded in 2026: Coder Agents GA 2026-09-09 ([Coder](https://coder.com/blog/coder-agents-ga)); IBM Bob self-hosted and air-gapped GA 2026-10-01 ([MarkTechPost](https://www.marktechpost.com/2026/10/02/ibm-brings-bob-to-self-hosted-and-air-gapped-environments/)); Cursor self-hosted machines from 2026-09-02 ([spotdev](https://www.spotdev.co.uk/blog/self-hosted-cursor-agents), secondary); Copilot coding agent on ARC self-hosted runners ([changelog](https://github.blog/changelog/2025-10-28-copilot-coding-agent-now-supports-self-hosted-runners), 2025-10-28); OpenHands Enterprise. Ona bet on self-hosted agent infra and was absorbed by OpenAI ([bex.co](https://bex.co/blog/2026/07/28/ona-openai-acquisition-self-hosted-agent-infra), 2026-07-28, secondary, dates conflict) |
| Generic "any task" agent | Devin, Factory, Codex, Cursor, Copilot. Heimdall has no edge in raw coding skill |

The pattern: the runtime and the agent are both commodity. What none of them sell is an **independent proof and a
human sign-off on the exact commit, inside the customer's network**. That is the asset.

---

## A. Is the thesis right?

**Half right.** The unit "a job whose outcome a machine can check" is correct. It lets cheap models work, because a
failed attempt costs cents and never reaches a person. It also makes outcome pricing honest.

**The part that breaks:** machine-checkable jobs are also the easiest for everyone else to automate. In 2026 GitHub,
Snyk, Datadog, CircleCI, Atlassian, GitLab and Sonar all shipped "agent fixes it until CI is green" (rows 1 and 2).
Trunk, which owned the flaky-test data, decided the fixing agent was not the moat. So "verifiable" alone does not make a
sellable product. It makes a feature.

**What survives:** sell verifiable jobs **where the incumbents cannot go**: self-hosted git (GitLab Self-Managed, GHES,
Bitbucket DC), air-gapped or "code must not leave" companies, and audit-bound teams. And make the proof itself the
product: Heimdall verifies, a person approves the exact commit, the record survives an audit. Heimdall's own agents are
the default worker, but the gate also accepts work from Renovate, Dependabot, Copilot or a person.

Better one-line positioning: **"Agents on your VM that only hand over work with proof, starting with the upgrade backlog
nobody wants to touch."**

**Paid wedge: Job 1s, dependency and CVE upgrade PRs that arrive green with proof, for self-hosted git.**
- Pain with a number and an owner: 278-day lag, 87% exploitable, a security SLA, and a stale Renovate queue the
  security lead can count.
- Proof is near total: CVE gone (E2 scanner), build green and tests green (E1), test files untouched (gate), human
  approves the exact commit.
- Cheap models fit: about half of upgrades need no model (46.9% baseline). The agent only works on the breaking rest.
- The gap is real: Renovate self-hosts but does not repair. GitHub's agent assignment is github.com only. Snyk, FOSSA,
  Socket and Sentry are SaaS.
- It reuses the merge gate. Every bot PR passes through the same ten rules.

**Next two:**
1. **Job 6a, bug to red repro test to fix.** It is Heimdall's red-first loop on a bug ticket instead of a feature ticket.
   Proof is pure E1. Sentry Seer does not run on self-hosted git. This is also the cleanest demo of "proof, not opinion".
2. **Job 3, characterization tests gated by mutation score.** It turns "green" into a stronger claim for jobs 1 and 6a,
   and it unlocks job 4 (migrations) later. Banks already pay for it (Diffblue).

**Not now:** flaky tests (crowded, statistical proof, needs real environment), SAST autofix (bundled into GitHub's $30
tier), migrations (needs many toolchains; AWS prices at a few dollars per upgrade), lint debt (no buyer).

## B. Pricing for outcome jobs

**Comparable price points**

| Model | Example | Source |
|---|---|---|
| Per merged change | Sourcegraph Agentic Batch Changes: pay only when a changeset merges; rate not public | [Sourcegraph](https://sourcegraph.com/blog/agentic-batch-changes-pricing), 2026-09-16 |
| Flat per PR, unlimited fix iterations | Gitar Enterprise; rate not public | [Sonar](https://www.sonarsource.com/products/gitar/compare/), accessed 2026-11-21 |
| Per fix run | Sentry Seer issue fix $1 per run from 2026-12-08 | [Gleap](https://www.gleap.ai/blog/sentry-pricing), secondary |
| Per review | GitLab $0.25 per review | [GitLab](https://about.gitlab.com/blog/agentic-code-reviews-with-flat-rate-pricing), 2026-03-19 |
| Per agent-minute | AWS Transform $0.035; about $2.52 per Java upgrade example | [Devoteam](https://www.devoteam.com/expert-view/aws-transform-and-agentic-modernisation/), secondary |
| Per agent compute unit | Devin $2.25 per ACU (about 15 minutes), replaced by quotas on 2026-04-14 | [replicas.dev](https://replicas.dev/resources/devin-pricing), secondary |
| Per committer or developer | GitHub Code Security $30; Snyk $25; Socket $25; Datadog Test Opt. $20; Seer $40 | rows above |
| Measured agent cost per merged PR | $7.64 (one developer's logs); about $16 (a tracker) | [DEV](https://dev.to/dspv/my-coding-agents-would-have-cost-8360-in-30-days-heres-where-it-went-3jil), [superpenguin](https://superpenguin.ai/coding-roi), 2026, secondary |
| Keep-old-version support | HeroDevs median $30,750 a year | [Vendr](https://www.vendr.com/marketplace/herodevs) |

**Proposal (to test with design partners, all not verified with buyers)**

| Tier | Price | What counts |
|---|---|---|
| Community | Free, 1 VM, 20 merged proof PRs a month | Trials, OSS |
| Team | $400 per VM per month, includes 50 merged proof PRs; then $6 per simple bump, $30 per breaking-change fix | Unmerged, failed or escalated attempts are free |
| Regulated | $24k a year floor: signed evidence export, control mapping, SSO, offline bundle, local models | Same per-PR overage |

Rules that keep outcome pricing honest:
- Bill on merge, and only if the change is not reverted within 7 days. A merged PR is easy to game without a window
  ([simplistic.cloud](https://simplistic.cloud/designing-outcome-based-pricing-for-ai-agents-in-developer-t), secondary).
- Print model cost per PR. Customer pays their own tokens (BYO key), as today.
- Sanity check: 50 developers on GitHub Code Security pay $1,500 a month. The Team tier at 80 merged fixes costs about
  $400 + 30 x $6 = $580 to $1,300 depending on the mix. A breaking fix at $30 is under one engineer hour at $100.
  Engineer hours per breaking upgrade: **not verified**, measure with partners.

## C. The 3-minute demo

Setting: a self-hosted GitLab (or GHES) repo in Java or TypeScript. A Renovate PR for a CVE fix needs a major bump. It is
red and 3 weeks old.

| Time | What the viewer sees |
|---|---|
| 0:00 | The org's Renovate queue: 40 open PRs, 12 red, oldest 90 days. One has a critical CVE |
| 0:20 | Heimdall on one VM picks the red PR. Deterministic step first: resolve versions, fetch into the local mirror, run the scanner. Model not called yet |
| 0:45 | The agent works in the no-network container. Live log in Slack: "compile errors 11, now 3, now 0" |
| 1:15 | The agent edits a test to make it pass. The gate rejects it on screen: "protected test changed, attempt void". It retries and fixes the code instead |
| 1:50 | Proof card on the PR: CVE before and after (E2), build green, tests green 3 runs (E1), test file hashes unchanged, diff 38 lines, model cost $0.31, 7 minutes |
| 2:20 | A person types `heimdall approve` in Slack. A second push shows the approval void. They approve again |
| 2:40 | Overnight result: 31 of 40 merged with proof, 9 escalated with a reason each. Bill shows 31 lines. Audit record exports and verifies offline |

The $0.31 and 7 minutes are placeholders. Measure them before showing.

## D. What would prove the thesis wrong

Run these with 3 design partners in 6 weeks. Any one of them kills or reshapes the wedge.

1. **Proof is not trusted.** Reviewers re-run and re-read Heimdall's green PRs as much as Renovate's. Signal: merge rate
   of proof PRs under 60%, or median time to approve not lower than for plain bot PRs.
2. **Cheap models fail on the part that matters.** Breaking-change success under 30% in partner repos (the Byam level),
   so cost per merged PR beats the $30 price. Easy bumps alone are already free with Renovate automerge.
3. **Self-hosted buyers wait for the bundle.** Partners say GitLab Duo Self-Hosted, IBM Bob or Copilot on ARC runners is
   "good enough", or will be within two quarters.
4. **Setup cost per repo beats the value.** Each repo needs a custom toolchain and registry mirror. If setup takes over a
   day per repo, or the no-network sandbox breaks too many builds, the one-VM story fails. Trunk hit this: an agent that
   works across architectures and repos is hard.
5. **Security wants "fix less", not "fix faster".** If buyers prefer reachability triage (only 18% of criticals stay
   critical) over upgrade PRs, the job is prioritization, not repair, and Endor, Datadog and Snyk own it.
6. **Nobody signs outcome pricing.** Procurement insists on seats. Then price per committer and drop the per-PR story.
7. **Partner tests are too weak.** If green builds in partner repos catch under half of injected breaking changes,
   "green" proves little. Then job 3 must ship first, and the wedge order is wrong.

---

## Not verified list

- Engineer hours per dependency upgrade or breaking-change fix. No survey found.
- All vendor prices marked "secondary": Snyk, Datadog, BuildPulse, Gitar per-PR rate, Moderne, Codemod, Mobb, Devin,
  OpenHands, Seer usage pricing, AWS Transform per-minute rate (AWS blog body failed to load).
- VulnBump agent pass rates beyond the 46.9% baseline. Localized pages disagree.
- Kong flaky-test results, read from a secondary summary.
- patchwave.ai: seen only in one HN comment. No other source.
- Market size of GitLab Self-Managed, GHES and Bitbucket DC customers.
- All prices in section B. They are proposals, not tested with buyers.
- Heimdall's own cost and success rate on any of these jobs. Nothing has run.
