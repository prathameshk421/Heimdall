# 03. Integrations research for Heimdall

Date: 2026-11-20. Method: 32 web searches. Numbers come mostly from secondary sources (blogs, aggregators). Each row notes the source. Where sources disagree, the row says so. Where I found no number, the row says "not found". Check vendor pages before you quote a price in a sales document.

Effort scale for a Go server: S = under 3 days, M = 1 to 2 weeks, L = 3 to 6 weeks, XL = more than 6 weeks.
Priority: P0 = build now, P1 = next, P2 = on demand, P3 = skip.

---

## 1. Git hosts

| Option | Adoption (number, source) | Surface | Effort | Priority |
|---|---|---|---|---|
| GitHub (App, not PAT) | 81% of respondents in the 2025 Stack Overflow survey [S1] | GitHub App: webhooks, installation tokens, Checks API, rulesets | M (today: polling plus PAT-like flow; App adds webhooks and bypass rules) | P0 |
| GitLab, self-managed | 36% (Stack Overflow 2025, same question) [S1] | REST API, webhooks, MR notes, commit statuses, External Status Checks (Ultimate only) [S2] | M | P0 |
| Azure DevOps | 16.6% [S1] | REST API, service hooks, PR status plus branch policy "status" [S3] | M | P2 |
| Bitbucket (Cloud and Data Center) | No Stack Overflow figure found. Data Center does not reach end of life in 2029, unlike other Atlassian Data Center products [S4] | Build status API, Code Insights, webhooks | M | P2 |
| Gitea / Forgejo / Codeberg | No adoption number found. Forgejo 17.0 adds workflow webhooks and a richer Actions API [S5] | GitHub-like REST API, commit status (pending, success, error, failure), webhooks with signature header [S5] | S to M (API is GitHub-shaped) | P1 |

Findings that change design:

- GitHub App vs PAT. GitHub tells you to build an App for any long-lived integration. A PAT is tied to one person and stops when that person leaves [S6]. An App can be a ruleset bypass actor. A PAT cannot [S6]. Recommendation: ship a GitHub App with a manifest flow (one click to create it) and keep a PAT mode for a trial only.
- Required checks. Since 2021 a ruleset can pin a required check to one App id [S7]. Heimdall should tell the user to pin `heimdall/gate` to the Heimdall App. This stops anyone from spoofing the check with a commit status.
- Merge queue. The queue needs checks on the `merge_group` event. A pass on the PR head commit does not count for the queue commit [S8]. Heimdall must answer `merge_group` `checks_requested` with a check run on `head_sha`, or the queue blocks with "Waiting for status to be reported". Path filters cause the same block, so Heimdall must always report (success or neutral/skipped counts as a pass) [S8].
- GitLab. External Status Checks work on self-managed, but only in the Ultimate tier. The feature flag went away in 15.9 and `pending` is supported since 16.5 [S2]. Many self-managed sites run Premium or Free. For them, use a commit status plus a "pipeline must succeed" rule, or an approval-rule bot. Document both paths.
- Forgejo has no ruleset system like GitHub. Use branch protection with a required status context.

---

## 2. CI systems and runners

| Option | Adoption (number, source) | Surface | Effort | Priority |
|---|---|---|---|---|
| GitHub Actions | 47% of organizations (JetBrains CI/CD survey 2026) [S9] | Composite action or Docker action that posts SARIF, coverage and test files to Heimdall, and fails or passes the job | S | P0 |
| GitLab CI | 32% of organizations [S9] | CI/CD component: YAML with `spec: inputs`, tagged with a semver, published to the Catalog [S10] | S | P1 |
| Jenkins | 28% of organizations [S9] | Shell step or shared library that calls the Heimdall CLI. A real plugin is Java. Skip the plugin | S (CLI only) | P2 |
| Buildkite | Not found | Plugin is a shell hook repo. A merge-queue blog shows Buildkite reporting checks to GitHub [S8] | S | P2 |
| CircleCI | Not found | Orb. Same CLI step works | S | P3 |
| Woodpecker | About 7.7k to 7.8k GitHub stars (undated, aggregators) [S11] | Plugin is a container image. It runs the Heimdall CLI | S | P2 |
| Drone | About 32k stars (one listing) [S11] | Same plugin image | S | P3 |
| Forgejo / Gitea Actions | Mostly compatible with GitHub Actions [S11] | Reuse the GitHub composite action | S (free) | P1 |
| Tekton, Argo Workflows, Dagger | No adoption number found | Container step or Dagger module that runs the Heimdall CLI | S each | P3 |

One design rule covers the whole table: ship one static `heimdall` CLI (Go, no CGO) with `heimdall ci report --sarif x.sarif --coverage c.out`. Every CI wrapper stays a 20-line shim around it. This is the cheapest way to cover 10 CI systems.

### Runner prices (2 vCPU Linux x64, per minute, secondary sources [S12])

| Provider | Price | Note |
|---|---|---|
| GitHub-hosted | $0.006 list. GitHub cut hosted prices about 40% from 2026-01-01 [S13] | Baseline |
| GitHub self-hosted fee | $0.002 per minute was announced for 2026-03-01, then suspended after backlash [S13][S14] | Status in Nov 2026 not confirmed. Check GitHub docs |
| Blacksmith | About $0.004. 3,000 free minutes per month [S12] | macOS costs more than GitHub |
| Ubicloud | $0.00125 to $0.002 (sources disagree) [S12] | Cheapest. Linux only |
| WarpBuild | $0.002 (own cloud) or $0.004 (managed) [S12] | Vendor claims it beats GitHub even after the fee |
| Depot | List rate $0.006, per-second billing [S12] | Plan-based pricing too |
| RunsOn | You pay AWS only, plus a flat yearly license [S12] | Spot instances. Fits "company-hosted" |
| actions-runner-controller, Namespace | Not priced here | ARC is free on your own K8s |

Implication: the CI wrapper must not depend on the runner. Heimdall runs gates on the company VM, so it also lowers CI minutes. Say this in sales copy, with a measured number from a real repo.

---

## 3. Deployment targets

| Target | What similar tools ship (source) | Effort | Priority |
|---|---|---|---|
| Single binary plus systemd | Gitea: binary plus unit file is the common path, plus Docker image and Helm chart [S15] | S (done in part) | P0 |
| Docker Compose | Sentry self-hosted is Compose only [S16]. PostHog recommends Compose and has sunset Kubernetes support [S16]. Infisical documents Compose for one host and Helm for HA [S16] | S | P0 |
| Helm chart | Gitea and Coder ship charts. Coder docs cover packages, Docker, binary, Helm, offline [S15] | M | P1 (only for HA or K8s-first buyers) |
| Offline bundle (tarball with images) | Coder documents offline installs [S15] | M | P1 |
| Hetzner and DigitalOcean one-click | Hetzner CX23 (2 vCPU, 4 GB) is about EUR 5.49 per month ex VAT after the 2026-06-15 price rise [S17]. Stock was reported short at times [S17] | S (cloud-init script, docs) | P1 |
| AWS Marketplace AMI | 20% listing fee on public server products. 3% to 1.5% on private offers by contract size [S18]. GCP and Azure fees: not verified | L | P3 until you have buyers asking |
| Nomad, Fly.io, Coolify, Proxmox | No data found | S each (a Compose file or LXC script covers most) | P3 |

Heimdall fits in 16 GB with room to spare (server about 42 MiB idle). The heavy parts are Postgres, FalkorDB and the sandbox. Offer a "lite" profile: one binary, Postgres, no Graphiti. This matches the "lighter than alternatives" story.

Lesson from PostHog and Sentry: a heavy stack with many services makes self-hosting hurt. Keep the stack to 2 or 3 containers.

---

## 4. Issue trackers

| Option | Adoption (number, source) | Surface | Effort | Priority |
|---|---|---|---|---|
| Jira | 46% in Stack Overflow 2025 [S1]. More than 300,000 organizations (single weak source) [S19] | REST API, webhooks, changelog (done: polling) | Done. Add webhooks M | P0 (keep) |
| GitHub Issues | Free with the git host. No number found | Same App token as the PR work | S | P0 |
| Linear | 1,351 new companies in Feb to Apr 2026 vs Jira 1,524. Growth +67% year on year [S19] | GraphQL API and webhooks | M | P1 |
| GitLab Issues | Rides on GitLab host support | Same token | S | P1 |
| Azure Boards | Rides on Azure DevOps | Same PAT or Entra token | S after host | P2 |
| Shortcut, YouTrack, Plane | No adoption number found. Plane star count not found | REST and webhooks each | M each | P3 |

Design hint: define one internal `WorkItem` port (key, title, status, links, comments). Jira, Linear and GitHub Issues then become small adapters.

---

## 5. Chat

| Option | Adoption (number, source) | Direct app or bridge | Effort | Priority |
|---|---|---|---|---|
| Slack | Estimates of 38 to 47 million daily users; no official figure since 2019 [S20] | Direct app. Use Socket Mode so no public URL is needed. Since 2026-03-03 non-Marketplace distributed apps get Tier 1 limits on `conversations.history` and `replies` (1 request per minute, 15 messages). Internal customer-built apps keep the old limits [S21] | M | P0 |
| Microsoft Teams | About 220 million daily users (mid-2025 aggregator, low confidence) [S20] | Direct bot needed for chat. Office 365 connectors were retired in May 2026. Workflows webhooks post as a generic bot with limited cards [S22]. Bot Framework SDK is archived. Microsoft points to the Teams SDK or the Microsoft 365 Agents SDK [S22] | L (Azure bot registration, public HTTPS) | P1 |
| Discord | Not found | Bridge through Switch (current) | Done | P2 |
| Mattermost | No share found. Runs on a 4 GB VPS for 50 to 100 users [S23] | Bridge (current) | Done | P2 |
| Google Chat, Zulip, Rocket.Chat, Matrix | No numbers found | Incoming webhook for notices is enough. Matrix and Zulip bridges only on demand | S for webhook only | P3 |

Decision: keep Switch as the bridge for long-tail chat. Build two direct paths only: a one-way "notify" webhook sender (Slack, Teams Workflows, Google Chat, Discord, Mattermost all take JSON posts) and a full two-way Slack app. A plain webhook sender covers 80% of the value for 5 platforms in S effort.

---

## 6. LLM providers and models

### Providers

| Option | Adoption / note (source) | Surface | Effort | Priority |
|---|---|---|---|---|
| OpenRouter | Today's path. One key, many models | OpenAI-compatible API | Done | P0 |
| OpenAI-compatible endpoint (generic base URL) | Covers vLLM, Ollama, LM Studio, llama.cpp, LiteLLM | `/v1/chat/completions` | S | P0 |
| Ollama | About 172k to 182k GitHub stars (varies by source) [S24] | OpenAI-compatible | S | P0 (via generic) |
| llama.cpp | About 100k to 129k stars [S24] | OpenAI-compatible server | S | P0 (via generic) |
| vLLM | About 91k to 93k stars (one source says 35k, likely stale) [S24] | OpenAI-compatible | S | P0 (via generic) |
| LiteLLM proxy | About 30.7k stars (undated, StackShare) [S24] | OpenAI-compatible gateway to Bedrock, Vertex, Azure | S | P1 (document as the enterprise route) |
| Anthropic, OpenAI direct | Vendor APIs | Native APIs | S each | P1 |
| Bedrock, Vertex, Azure OpenAI | Needed by regulated buyers | Use LiteLLM first. Native later | M each | P2 |

Rule: build one provider interface, one OpenAI-compatible adapter, and document LiteLLM for the cloud-vendor cases. This avoids three SDKs.

### Cheap open models for code review (late 2026, secondary sources [S25][S26][S27])

No source I found measures code review directly. SWE-bench numbers measure bug fixing. Treat them as a proxy only. SWE-bench Verified was reported as archived by Vals AI in Sep 2026 as saturated, so SWE-bench Pro is the newer proxy [S25].

| Model | Reported score | Price per 1M tokens (in / out) | Licence or note |
|---|---|---|---|
| DeepSeek V4 Flash (Heimdall default) | Not found | OpenRouter pages show $0.065 to $0.14 in, $0.17 to $0.45 out, by snapshot. About 1M context [S26] | Cheapest credible. Cache reads $0.02 |
| DeepSeek V4 Pro | 80.6% SWE-bench Verified [S25] | $0.435 / $0.87 on one list [S25] | MIT |
| Kimi K2.6 | 80.2% Verified [S25] | Not found | Open weights |
| MiniMax M2.5 | 80.2% Verified [S25] | Not found | M3 weights pending in one source |
| GLM-5 / GLM-5.2 | 77.8% Verified (GLM-5). GLM-5.2 62.1% on SWE-bench Pro vs 55.4% DeepSeek [S25] | Not found | Verbose: about 43k output tokens per task, so real cost is higher |
| Devstral 2 (123B) | 72.2% Verified [S27] | $0.40 / $2.00 after the free period [S27] | Modified MIT, paid licence above $20M monthly revenue |
| Qwen3-Coder-Next | 70.6% Verified [S25] | $0.11 to $0.22 in, $0.80 to $0.88 out (sources differ) [S25] | 80B total, 3B active. Runs locally |
| gpt-oss-120b | Not found | About $0.04 in [S27] | Apache 2.0 |

Advice: keep DeepSeek V4 Flash as default. Offer a "bring your own endpoint" setting for Qwen3-Coder-Next on a local GPU or Ollama. Run Heimdall's own review eval (a fixed set of seeded-bug PRs) on 3 models before you claim a winner. Do not publish a leaderboard from blog numbers.

---

## 7. Agent runtimes and protocols

| Option | Adoption (number, source) | Surface | Effort | Priority |
|---|---|---|---|---|
| OpenCode | About 172k to 193k GitHub stars (Jun to Aug 2026) [S28] | CLI, `opencode.json`, server mode | Done | P0 |
| Claude Code | About 131k to 140k stars [S28] | CLI, headless mode, MCP | S (adapter) | P1 |
| Codex CLI | About 90k to 104k stars [S28] | CLI exec mode | S | P1 |
| Gemini CLI | About 105k stars [S28] | CLI | S | P2 |
| Cline | About 63k stars [S28] | CLI | S | P3 |
| Goose | Founding project of the Agentic AI Foundation [S29]. Star count not found | CLI | S | P3 |
| Aider, OpenHands | Not found | CLI / server | S each | P3 |
| MCP | More than 10,000 published servers (Dec 2025). Adopted by Claude, Cursor, Copilot, Gemini, VS Code, ChatGPT. Now under the Linux Foundation Agentic AI Foundation [S29] | Server (expose Heimdall findings and gate state as tools) and client | M | P1 |
| AGENTS.md | More than 60,000 projects [S29] | A file. Read it as policy input and write a Heimdall section | S | P1 |
| A2A | No 2026 adoption data found | HTTP agent cards | M | P3 |

Idea: expose a Heimdall MCP server ("list findings", "explain gate", "uphold F1"). Any coding agent can then ask Heimdall before it opens a PR. This is cheap and fits the way teams already work.

---

## 8. Static analysis, security and supply chain

All tools below run offline in the sandbox with `--network=none` if you pre-pull rules and databases.

| Tool | Signal (source) | SARIF | Effort | Priority |
|---|---|---|---|---|
| Opengrep / Semgrep CE | Both LGPL 2.1. Semgrep community rules sit under the Semgrep Rules License, which limits commercial reuse [S30] | Yes (2.1.0) [S30] | S | P0. Prefer Opengrep with your own rules |
| golangci-lint, ruff, eslint, gosec, Bandit | Language linters. No adoption numbers collected | Most emit SARIF or convert | S each | P0 for Go, ruff, eslint. P1 for others |
| Trivy | About 32k to 36k stars [S31] | Yes | S | P0 |
| Gitleaks | About 25k to 28k stars [S31] | Yes | S | P0 |
| TruffleHog | 17k to 26k stars (sources disagree) [S31]. Verifies secrets by calling APIs, which needs network | Listed in one source only | S | P2 offline |
| Grype and Syft | Grype 9k to 12k stars. Syft figure unreliable [S31] | Grype yes | S | P1 (SBOM plus match) |
| OSV-Scanner | 6k to 10k stars (sources disagree) [S31] | SARIF not confirmed | S | P1 |
| Checkov, tfsec, hadolint, Bearer, ast-grep, reviewdog | No numbers collected | Most emit SARIF | S each | P2 |
| CodeQL | CLI is free on public repos. Private repos need a GitHub Code Security licence [S32]. Price not found | Yes | M | P3 (licence blocks a bundled product) |
| Sigstore cosign, GitHub artifact attestations, SLSA | `gh attestation verify` works offline with `--bundle` and `--custom-trusted-root`. Public repos use the Sigstore public instance. Private repos use GitHub's own instance, with no transparency log [S33] | n/a (verify step) | M | P1 |
| OpenSSF Scorecard, in-toto | Not researched in depth | Scorecard emits SARIF | S | P2 |

Design hint: Heimdall already ingests SARIF. Make SARIF the one input format. Then "support a new scanner" means "document the command line", not "write code". Ship a `tools.yaml` catalogue with pinned versions and the exact command for each scanner.

Licence note: do not bundle CodeQL. Do not bundle Semgrep rules. Opengrep plus your own rules or permissive rule sets keeps you free to sell.

---

## 9. Test tooling

| Option | Signal (source) | Surface | Effort | Priority |
|---|---|---|---|---|
| Go coverage (`-coverprofile`) | Built into Go | File parse | S | P0 |
| Diff coverage (coverage on changed lines only) | Codecov is strong at this. Self-hosted Codecov has a 50-user minimum in one listing [S34] | Parse cover profile plus the PR diff | M | P0. This is a gap in small-team tools and fits the gate |
| Mutation: Stryker (JS, TS, .NET), PIT (Java), mutmut (Python), cargo-mutants (Rust, Thoughtworks Radar "Trial", Apr 2026), go-mutesting (no source found) | Run on changed files only in PRs, full run later [S35] | CLI plus score threshold | M (runner in sandbox, parse score) | P1 |
| Self-hosted coverage services (gocov: single binary plus Postgres, AGPL-3.0; OpenCov flagged discontinued) | Maturity unconfirmed [S34] | n/a | n/a | P3. Do not depend on them |
| Flaky detection | No source found | Compare reruns of the same SHA from CI results | M | P2 |
| Test impact analysis | No source found | `go list` dependency graph for Go | L | P3 |

---

## 10. Observability

| Option | Signal (source) | Effort | Priority |
|---|---|---|---|
| Prometheus `/metrics` | Prometheus exporters are the most common infra method (72%) in the OTel 2026 survey [S36] | S | P0 |
| Structured logs (`slog` JSON) | Standard library | S | P0 |
| OpenTelemetry traces and metrics (OTLP) | OTel used by 57% (metrics), 50% (traces), 48% (logs) in Grafana's 2026 survey [S36]. Go Logs API and SDK are at release candidate, not stable [S36] | M | P1 |
| Grafana dashboards | Ship one JSON dashboard in the repo | S | P1 |

Keep OTel optional and off by default to protect the 42 MiB footprint. Expose the outbox depth, gate latency, LLM cost per PR and sandbox failures as the four key metrics.

---

## 11. Policy as code

| Option | Signal (source) | Effort | Priority |
|---|---|---|---|
| CODEOWNERS | GitHub and GitLab read it natively. Heimdall should read it as the "who must approve" source | S | P0 |
| Heimdall policy file in the repo (today, read from merge base) | Own format | Done | P0 |
| OPA / Rego (and Conftest) | General-purpose engine. Used with Conftest for config rules [S37]. Embedding in Go: not verified in this research | M | P1 (as an optional rule type) |
| Kyverno | CNCF graduated 2026-03-16. Kubernetes only, YAML [S37] | n/a | P3 |
| Cedar | AWS-origin. Ecosystem limited vs OPA [S37] | M | P3 |

---

## Top 15 integrations to build, in order

1. **GitHub App (manifest flow, webhooks, pinned required check).** Needed for ruleset bypass and for merge queue. 81% of developers use GitHub [S1].
2. **GitHub merge queue (`merge_group` checks).** Without it, Heimdall blocks teams that use a queue. Small effort once the App exists [S8].
3. **`heimdall` CLI plus GitHub composite Action.** One CLI feeds every CI system. Cheap and reaches 47% of organizations [S9].
4. **Single binary plus systemd and Docker Compose, with a lite profile.** Matches what Sentry, PostHog and Infisical ship [S16]. Lighter than rivals is the pitch.
5. **Generic OpenAI-compatible LLM endpoint (Ollama, vLLM, llama.cpp, LiteLLM).** One adapter covers all local and gateway cases. Gives an air-gapped option.
6. **SARIF scanner catalogue: Opengrep, Trivy, Gitleaks, golangci-lint, ruff, eslint.** Free to bundle. Skips CodeQL and Semgrep rules for licence reasons [S30][S32].
7. **Diff coverage gate from Go cover profiles.** Few small-team tools do it well. Core to "gate" value.
8. **GitLab self-managed (commit status plus External Status Checks where Ultimate).** 36% usage [S1]. Self-managed is the sweet spot for company VMs. Needs a fallback for non-Ultimate [S2].
9. **Slack app over Socket Mode, plus a generic webhook notifier.** Socket Mode needs no public URL. The notifier covers Teams, Google Chat, Discord, Mattermost cheaply [S21][S22].
10. **GitHub Issues and a `WorkItem` port (then Linear).** Same token as step 1. Linear is the fastest-growing tracker [S19].
11. **Mutation testing on changed files (Stryker, PIT, mutmut, cargo-mutants).** Strong proof for AI-written tests. Runs in the existing sandbox [S35].
12. **Prometheus metrics, `slog` JSON logs and a Grafana dashboard JSON.** Small. Buyers ask for it in security reviews. OTel after this [S36].
13. **Heimdall MCP server and AGENTS.md support.** Lets Claude Code, Codex and OpenCode ask Heimdall for gate state. MCP is now a Linux Foundation project [S29].
14. **Forgejo/Gitea host adapter.** GitHub-shaped API and a loyal self-hosting audience. Reuses the GitHub Action through Forgejo Actions [S5][S11].
15. **Attestation verify step (`gh attestation verify --bundle`, cosign) plus Helm chart and offline bundle.** Wins regulated and air-gapped buyers. Do last, because it needs the earlier pieces [S33].

Deliberately not on the list: AWS Marketplace (20% fee [S18]), CodeQL, Teams bot (build only when a buyer asks, since the Bot Framework SDK is archived [S22]), Bitbucket and Azure DevOps (add on request; both are M effort and 16.6% or lower in usage [S1]).

---

## Open questions (not verified)

- Whether GitHub restarted the $0.002 self-hosted runner fee after suspending it [S14].
- GCP and Azure marketplace fees. Azure DevOps Server support of the newest policy API.
- Linear and Plane API limits. Star counts for Plane, Goose, Aider, OpenHands.
- Whether OPA embeds cleanly in a CGO-free Go build.
- Real code-review quality of each model. Needs Heimdall's own eval.
- DigitalOcean 16 GB price (not found). Hetzner stock and price move often [S17].

## Sources

- [S1] Stack Overflow Developer Survey 2025 and coverage: https://survey.stackoverflow.co/2025/technology/ , https://stackoverflow.co/company/press/archive/stack-overflow-2025-developer-survey/ , https://dev.to/davidshq/thoughts-on-stackoverflow-2025-developer-survey-part-1-3d5o
- [S2] GitLab External Status Checks: https://docs.gitlab.com/user/project/merge_requests/status_checks/ , https://docs.gitlab.com/api/status_checks/
- [S3] Azure DevOps PR status policy: https://learn.microsoft.com/en-us/azure/devops/repos/git/pr-status-policy , https://learn.microsoft.com/en-us/azure/devops/release-notes/2026/repos/sprint-273-update
- [S4] Atlassian Data Center end of life: https://www.atlassian.com/licensing/data-center-end-of-life , https://www.atlassian.com/migration/bitbucket-hybrid-license
- [S5] Forgejo: https://alternativeto.net/news/2026/10/forgejo-17-0-adds-workflow-webhooks-and-job-summaries/ , https://forgejo.org/2026-05-monthly-report/ , https://hookdeck.com/webhooks/platforms/guide-to-forgejo-webhooks-features-and-best-practices
- [S6] GitHub, deciding when to build a GitHub App: https://docs.github.com/en/enterprise-server@3.21/apps/creating-github-apps/about-creating-github-apps/deciding-when-to-build-a-github-app , https://docs.github.com/en/enterprise-cloud%40latest/rest/repos/rules , https://findy-network.github.io/blog/2024/03/27/managing-github-branch-protections
- [S7] GitHub changelog, required checks pinned to an app: https://github.blog/changelog/2021-11-30-ensure-required-status-checks-provided-by-the-intended-app/
- [S8] Merge queue and required checks: https://docs.github.com/en/pull-requests/how-tos/merge-and-close-pull-requests/troubleshooting-required-status-checks , https://github.blog/changelog/2022-08-18-merge-group-webhook-event-and-github-actions-workflow-trigger , https://oneuptime.com/blog/post/2026-09-16-configure-buildkite-required-checks-github-merge-queues/view
- [S9] JetBrains State of CI/CD 2026: https://www.jetbrains.com/pages/state-of-cicd2026/ , https://jenkins.io/blog/2026/04/06/jetbrains-report-highlights-jenkins-as-a-popular-tool-in-2026
- [S10] GitLab CI/CD components: https://docs.gitlab.com/ci/components
- [S11] Woodpecker and Drone: https://github.com/woodpecker-ci/woodpecker/releases , https://raw.githubusercontent.com/woodpecker-ci/woodpecker/main/README.md , https://startupstash.com/alternatives/drone-io/
- [S12] Runner prices: https://dev.to/kalemi/the-github-actions-cost-index-2026-h1g , https://betterstack.com/community/comparisons/github-actions-runner/ , https://warpbuild.com/blog/github-actions-price-change , https://dev.to/kalemi/blacksmith-ci-pricing-explained
- [S13] GitHub pricing change: https://github.blog/changelog/2025-12-16-coming-soon-simpler-pricing-and-a-better-experience-for-github-actions/ , https://resources.github.com/actions/2026-pricing-changes-for-github-actions/
- [S14] Self-hosted fee suspended: https://www.theregister.com/2025/12/17/github_charge_dev_own_hardware/ , https://runs-on.com/blog/github-self-hosted-runner-fee-2026/
- [S15] Gitea and Coder install: https://docs.gitea.com/category/installation , https://coder.com/docs/install/server/kubernetes.md , https://docs.renovatebot.com/examples/self-hosting/
- [S16] Self-host patterns: https://posthog.com/docs/self-host , https://docs.sentry.io/server/ , https://infisical.com/docs/self-hosting/deployment-options/docker-compose
- [S17] Hetzner: https://www.bitdoze.com/hetzner-cloud-review/ , https://betterstack.com/community/guides/web-servers/hetzner-cloud-review/ , https://docs.hetzner.com/general/infrastructure-and-availability/price-adjustment/
- [S18] AWS Marketplace fees: https://docs.aws.amazon.com/marketplace/latest/userguide/listing-fees.html , https://clazar.io/blog/aws-marketplace-fees
- [S19] Jira vs Linear: https://bloomberry.com/blog/the-data-on-jira-vs-linear-whos-winning-new-customers/ , https://tech-insider.org/?p=3664
- [S20] Slack and Teams users: https://tech-insider.org/slack-vs-microsoft-teams-2026/ , https://sqmagazine.co.uk/slack-vs-microsoft-teams-statistics
- [S21] Slack rate limits: https://docs.slack.dev/changelog/2025/05/29/rate-limit-changes-for-non-marketplace-apps , https://docs.slack.dev/apis/web-api/rate-limits
- [S22] Teams connectors and SDKs: https://devblogs.microsoft.com/microsoft365dev/retirement-of-office-365-connectors-within-microsoft-teams/ , https://mc.merill.net/message/MC1181996 , https://learn.microsoft.com/en-us/answers/questions/5767488/teamsfx-bot-stopped-working-(nullreferenceexceptio
- [S23] Self-hosted chat: https://dev.to/selfhostingsh/best-self-hosted-communication-chat-tools-in-2026-4m77 , https://resources.rework.com/tools/productivity/mattermost-vs-rocket-chat
- [S24] Local LLM tools: https://tech-insider.org/vllm-vs-ollama-2026/ , https://stackshare.io/litellm/vs/ollama , https://aithinkerlab.com/llama-cpp-100k-github-stars-2026/
- [S25] Open coding models: https://www.morphllm.com/best-open-source-coding-model-2026 , https://tech-insider.org/best-open-source-llm-2026/ , https://tech-insider.org/glm-5-2-vs-deepseek-v4-vs-kimi-k2-2026/ , https://www.morphllm.com/minimax-m2-5-coding
- [S26] OpenRouter DeepSeek V4 Flash: https://openrouter.ai/deepseek/deepseek-v4-flash , https://openrouter.ai/models/deepseek/deepseek-v4-flash-0731
- [S27] Devstral 2 and gpt-oss: https://mistral.ai/news/devstral-2-vibe-cli , https://the-decoder.com/mistrals-open-coding-model-devstral-2-claims-sevenfold-cost-advantage-over-claude-sonnet/ , https://pricepertoken.com/compare/mistral-ai-devstral-2512-vs-openai-gpt-oss-120b
- [S28] Coding agent stars: https://www.morphllm.com/best-ai-cli-tools-2026 , https://github.com/jqueryscript/awesome-coding-agent , https://www.augmentcode.com/learn/claude-code-131k-stars
- [S29] MCP and Agentic AI Foundation: https://www.linuxfoundation.org/press/linux-foundation-announces-the-formation-of-the-agentic-ai-foundation
- [S30] Opengrep vs Semgrep: https://docs.semgrep.dev/faq/comparisons/opengrep , https://blog.codacy.com/opengrep-vs-semgrep , https://www.infoq.com/news/2025/02/semgrep-forked-opengrep
- [S31] Security tool stars: https://appsecsanta.com/state-of-open-source-appsec-2026 , https://appsecsanta.com/open-source-tools , https://appsecsanta.com/trivy-vs-grype
- [S32] CodeQL licence: https://docs.github.com/en/code-security/concepts/code-scanning/codeql/codeql-cli
- [S33] Attestations offline: https://docs.github.com/en/enterprise-cloud@latest/actions/how-tos/secure-your-work/use-artifact-attestations/verify-attestations-offline , https://cli.github.com/manual/gh_attestation_verify
- [S34] Coverage tools: https://alternativeto.net/software/codecov-io , https://pkg.go.dev/github.com/gocov/gocov@v0.11.0 , https://alternativeto.net/software/opencov
- [S35] Mutation testing: https://www.thoughtworks.com/radar/tools/cargo-mutants , https://docs.rs/crate/cargo-mutants/latest/source/README.md , https://www.augmentcode.com/guides/mutation-testing-ai-generated-code
- [S36] Observability: https://opentelemetry.io/blog/2026/otel-prometheus-interoperability/ , https://grafana.com/blog/observability-survey-OSS-open-standards-2026 , https://opentelemetry.io/blog/2026/go-logs-api-sdk-rc/
- [S37] Policy engines: https://www.harness.io/blog/policy-as-code-in-2026-opa-kyverno-cedar-and-what-s-next , https://nirmata.com/2026/01/28/whats-the-difference-between-kyverno-and-opa-gatekeeper/ , https://www.pulumi.com/blog/best-policy-as-code-tools/
