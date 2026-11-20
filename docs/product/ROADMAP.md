# Heimdall roadmap (draft 1, before the critic)

Strategy: `STRATEGY.md`. Ideas: `IDEAS.md`. Critic verdicts: `CRITIC.md`. Delegation log: `TRACKER.md`.

Each work package (WP) is one concern and one branch. It names its owner model, its acceptance check and its files, so
two delegates never touch the same package. Order inside a stage is the build order.

## Stage 0: the proof loop works on real systems (in flight, other session)

The SOTA quality plan (`docs/SOTA_QUALITY_PLAN.md`) on `heimdall-build`. Exit: fresh ticket to merged PR and a
ticketless PR on the real VM with no manual repair; D6 (reviewer output limit) and D7 (ticketless PR never reviewed)
fixed; the unbounded seat reassignment bounded.

Product work below starts in parallel only where it does not touch the packages Stage 0 is changing.

## Stage 1: anyone can install it (adoption)

| WP | What | Owner | Accept | Touches |
|---|---|---|---|---|
| 1.1 | **Job runtime for agents.** A new `ports.AgentRuntime` adapter that runs one task as one headless CLI process (OpenCode `run`, Claude Code `-p`, Codex `exec`) inside the rootless runner, with the MCP token scoped to the task, then exits | Opus design, DeepSeek build | A test task and a review task complete through the new adapter on a fixture repo; no resident unit | `internal/adapters/jobagent` (new), `cmd/heimdall-runner` |
| 1.2 | **Config: Switch optional.** `agent_runtime.provider` accepts `job`; `switch.*` required only when Switch is used | Sonnet | Config tests for both modes | `internal/platform/config` |
| 1.3 | **PR as the primary surface.** Proof summary comment (one page, updated in place), `/heimdall` commands from PR comments by mapped GitHub logins | Opus design, Sonnet build | Component test with the GitHub fake; summary has proven / not proven / needs a person | `internal/app/announce`, `internal/adapters/github` |
| 1.4 | **Native Slack app (Socket Mode) and a generic webhook notifier** behind `ports.Chat` | Sonnet | Contract tests shared with the Switch adapter | `internal/adapters/slack`, `internal/adapters/webhook` (new) |
| 1.5 | **Ticketless default.** `require_ticket=false` default; a ticket adds context when the PR names one | Opus (gate rules) | Golden flow for a ticketless PR reaches review | `internal/domain/change`, policy |
| 1.6 | **Lite compose and `heimdall init`.** Three services; memory stack as a compose profile; `init` writes config, makes the GitHub App by manifest, checks the model key, runs doctor | Sonnet | Fresh Ubuntu VM to first gated PR in under 15 minutes, timed | `deploy/`, `cmd/heimdall` |
| 1.7 | **Any OpenAI-compatible model endpoint.** Remove the two hardcoded provider names; a base URL plus key per role | Haiku | Config and probe tests against an httptest server | `cmd/heimdall-server/model_provider.go`, `internal/adapters/openrouter` (rename `openai`) |
| 1.8 | **Language profiles.** Python (pytest), Node/TS (vitest/jest), Java (maven/gradle), Rust (cargo) runner profiles with pinned images and cached deps | Sonnet | Each profile runs a fixture repo's suite red then green in the sandbox | `deploy/runner-profiles` (new), `internal/adapters/rootlessrunner` |
| 1.9 | **GitHub merge queue.** Answer `merge_group` with a check run on its head SHA | Sonnet | Component test with a recorded `merge_group` payload | `internal/adapters/github`, `internal/app/prsync` |
| 1.10 | **Shadow mode.** Gate reports `neutral` and comments "would block because"; weekly digest of what it would have blocked | Sonnet | Golden test of the shadow check run | `internal/app/gateuse` |

## Stage 2: it is provably good and cheap (quality and cost)

| WP | What | Owner | Accept |
|---|---|---|---|
| 2.1 | **Proof Bench.** Replay bug-introducing PRs from real OSS repos and clean merged PRs; score recall, precision, evidence rate, cost per PR, wall time | Opus design, DeepSeek runs | First run on 10 repos, results in `docs/sota/RUNS.md` |
| 2.2 | **Red-first bench.** Issue plus fix pairs; the generated test must fail before the fix and pass after | Opus design, DeepSeek runs | First run, numbers recorded |
| 2.3 | **Deterministic skip.** No model call for docs-only, lockfile-only, generated, or under-N-line changes; policy-configurable | Sonnet | Unit tests on the classifier; bench shows $0 on those PRs |
| 2.4 | **Token budget and cost line.** Hard per-PR budget, `budget hit` status, cost printed in the check summary | Sonnet | Unit tests; cost line visible on a bench PR |
| 2.5 | **Prompt shape for caching.** Stable prefix (skill, repo map, rules), variable suffix (diff); record cache hit rate | Opus | Bench shows cache-read share and cost before/after |
| 2.6 | **Analyzers first.** Bundled SARIF tools per language (Opengrep, gitleaks, Trivy, golangci-lint, ruff, eslint) run before any model and feed E2 | Sonnet | Profile tests; E2 findings on a planted bug |
| 2.7 | **Test integrity checks.** Pinned red tests are read-only to the author; mock-of-subject detector; diff coverage; mutation score on changed lines as optional E2 | Opus (rules), Sonnet (build) | Unit tests on each detector with real tautological test samples |
| 2.8 | **Model routing per role.** Flash for triage and refute, stronger model for proof writing; per-role config | Sonnet | Bench cost and recall per routing choice |
| 2.9 | **Footprint budget.** Measure RAM and CPU of the Lite stack idle and under a 10-PR burst; fix the top 3 hot spots | Opus | Numbers in `docs/sota/performance.md` with machine and commit |

## Stage 3: the regulated buyer says yes (trust and compliance)

| WP | What | Owner | Accept |
|---|---|---|---|
| 3.1 | **Board auth.** OIDC login (GitHub, Google, Okta via generic OIDC), roles: viewer, approver, admin | Opus (security) | Security tests: no route readable without a session |
| 3.2 | **Signed change record and offline verifier.** One export per merge (events, evidence, approver, SHAs) signed with cosign-compatible keys; `heimdall verify record.json` | Opus | A tampered record fails verify in a test |
| 3.3 | **Audit pack.** Control-mapped report per period: SOC 2 CC8.1, ISO 27001 A.8.32/A.8.29, PCI DSS 6.2.3.1/6.5.1, DORA | Opus writes, a GRC reviewer checks | Sample pack reviewed by one auditor (not verified until then) |
| 3.4 | **Threat model and self-audit.** Prompt injection, sandbox escape, token scope, board, supply chain; Go fuzzing on parsers; escape tests in CI | Opus | Threat model doc; fuzz targets in `make verify` |
| 3.5 | **Release hygiene.** SBOM, cosign-signed images, SLSA provenance, offline bundle | Sonnet | Release workflow produces all four |
| 3.6 | **AI provenance.** Detect agent-written commits (trailers, session ids) and require E1 for their blocking areas | Opus | Unit tests on detection |
| 3.7 | **Observability.** Prometheus metrics, slog JSON, Grafana dashboard JSON | Haiku | Metrics endpoint test |

## Stage 4: wider reach (only what the core needs)

| WP | What |
|---|---|
| 4.1 | GitLab self-managed adapter (commit status, external status checks on Ultimate, MR notes) |
| 4.2 | Heimdall MCP server for coding agents: "what proof is missing on my PR" |
| 4.3 | AGENTS.md / CLAUDE.md / CONTRIBUTING rules read as rule cards |
| 4.4 | Linear and GitHub Issues behind a `WorkItem` port |
| 4.5 | Forgejo/Gitea adapter |
| 4.6 | Helm chart |
| 4.7 | Personal context packs, live (the later moat) |

## Go-to-market track (runs beside the build)

| Step | When |
|---|---|
| Landing page with measured numbers only (cost per PR, RAM, recall on Proof Bench) | after 2.1 |
| Show HN: "a merge gate that drops AI review findings without proof"; demo a tampered record failing verify | after 1.6 and 3.2 |
| Three design partners in fintech or EU finance at $5k for 90 days | after Stage 1 |
| Free for OSS maintainers via shadow mode on public repos | after 1.10 |
