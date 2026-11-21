# Heimdall roadmap (draft 3)

Strategy: `STRATEGY.md` (draft 3; read its section 0). Delegation log: `TRACKER.md`. Rules for delegates: `DELEGATE_PREAMBLE.md`.

Build order follows the loop in `STRATEGY.md §3`: prove it on real code, make the proof check land on any PR, open the
memory door, then the VM with jobs, then the regulated upsell. Each work package (WP) is one concern, one branch, with
named files so two delegates never collide. Integration branch: `product-build` (own worktree). Merges into
`heimdall-build` only after telling the Stage 0 session.

## Stage 0: the loop works live (other session, in flight)

The SOTA quality plan on `heimdall-build`: a fresh ticket and a ticketless PR reach a merge on the real VM with no
manual repair; D6 (reviewer output limit) and D7 (ticketless PR never reviewed) fixed; seat reassignment bounded.
Product WPs avoid the packages that session is changing (`internal/domain/change`, `internal/adapters/postgres`,
`internal/app/knowledge`, `internal/app/reconcile` until it says otherwise).

## Stage A: proof on any PR (weeks 1 to 4)

| WP | What | Owner | Accept |
|---|---|---|---|
| A0 | **Proof Bench smoke, week 1.** Dataset of real OSS bugs and clean PRs, each case proven real (fail before, pass after); then a 20-PR reviewer run with recall, precision, evidence rate, cost, p95 time | Opus design (P2.1a running), DeepSeek runs | `docs/sota/BENCH.md` with numbers, cost stated before the run (cap $3) |
| A1 | **Action mode design.** How the existing engine runs as a stateless step in the customer's CI: inputs, where state lives (check runs, PR comment, artifact), which packages are reused, the fast check / proof run split, the `unknown` verdict, model key handling, cost cap | Opus | `docs/product/design/ACTION_MODE.md` reviewed |
| A2 | **Fast check.** Deterministic, cached, under 2 minutes: secrets, SARIF analyzers, pinned-test guard, diff coverage, approval on head SHA, memory rules for changed paths | Opus rules, Sonnet build | p95 on the bench under 2 minutes |
| A3 | **Proof summary comment.** One comment updated in place: proven, dropped (count and reason), needs a person, not checked, cost. Pure renderer from the view | Sonnet | Golden tests on 5 real bench cases |
| A4 | **Shadow mode.** Check reports neutral with "would block because"; weekly digest | Sonnet | Golden test of the shadow check run |
| A5 | **Prove or drop third-party findings.** Read CodeRabbit, Copilot, Bugbot, Greptile review comments; each becomes a candidate finding that must earn E1 or E2 or is dropped and counted | Opus | Component test with recorded comments from each tool |
| A6 | **Test integrity.** Pinned red tests read-only to the author; flag a test that mocks a file the PR changed; a rerun-pass is never a clean pass | Opus rules, Sonnet build | Unit tests on real tautological test samples |
| A7 | **`heimdall action` and a composite GitHub Action.** One static binary, BYO model key, no database | Sonnet after A1 | A public fixture repo gets a proof summary in under 10 minutes from install |
| A8 | **Break-glass.** Bypass needs a reason and a named person, is recorded, shows as open until reviewed | Opus | Unit tests; shows in the summary and the record |
| A9 | **Board auth.** OIDC login (GitHub, generic OIDC), roles viewer / approver / admin; no route readable without a session | Opus (security) | Security tests on every route |
| A11 | **Stricter proof rule.** A finding's test fails twice on the head and passes on a tree with the suspected lines reverted or fixed; one red run is not proof; "unproven, not shown" wording | Opus | Unit tests on the rule; bench evidence rate before and after |
| A12 | **p95 and misses report.** Fast check and proof run p95 on the bench; a "what we did not check" section in every summary and a weekly misses report from bench and pilot data | Sonnet | Numbers in `docs/sota/BENCH.md`; golden test of the section |
| A13 | **Test selection for big repos.** Run only tests affected by the change (Go package graph first; `go list -deps`), full suite nightly | Sonnet | p95 on a large Go repo before and after |
| A10 | **Cost line and cap.** Per-PR token budget, `budget hit` status, cost in the summary, monthly cap per repo; deterministic skip for trivial PRs | Sonnet | Unit tests; $0 on docs-only bench PRs |

## Stage B: memory, enforced (weeks 3 to 8; B4, B6, B7 wait until a pilot asks)

| WP | What | Owner | Accept |
|---|---|---|---|
| B1 | **Session readers.** OpenCode (SQLite, CGO-free) and Cursor readers next to the Claude Code and Codex ones; worktree-aware | Sonnet | Reader tests on recorded fixtures (no real user data in the repo) |
| B2 | **Redaction on the laptop.** gitleaks rules plus PII redaction before any text leaves the reader; a redaction report | Opus (security) | Tests with planted secrets and PII |
| B3 | **Approve flow.** Terminal UI to accept, edit or reject each candidate card; nothing is shared unapproved | Sonnet | Test of the state rules |
| B4 | **Local MCP server and rule-file export.** Agents read approved cards (MCP); export to AGENTS.md, CLAUDE.md, Cursor rules | Sonnet | Contract test with an MCP client |
| B5 | **Signed sync to the server.** DSSE envelope with ed25519; cards scoped to code their owner approves at the merge base; E3 check in the fast check | Opus | Tampered card rejected in a test |
| B6 | **Card outcomes.** Kept, dismissed, overridden per card, shown to the card's owner only | Sonnet | Unit tests; no manager view exists |
| B7 | **Open-source split.** `heimdall memory` builds and ships alone, Apache-2.0 | Haiku | Separate build target and README |

## Stage C: the company VM and proof jobs (starts only with one signed self-hosted partner)

| WP | What | Owner | Accept |
|---|---|---|---|
| C1 | **Agents as jobs.** One task, one headless CLI process in the sandbox, task-scoped token, then exit; Switch optional | Opus design, DeepSeek build | Test and review task complete on a fixture with no resident unit |
| C2 | **Lite compose and `heimdall init`.** Server, Postgres, runner; memory stack as a profile; GitHub App by manifest | Sonnet | Fresh VM to first proof summary under 30 minutes, timed |
| C3 | **Runner profiles.** Python and Node/TS first, pinned images, cached deps, package mirror fetch step | Sonnet | Fixture suites red then green in the sandbox |
| C4 | **Upgrade job.** Take a red Renovate/Dependabot PR, fix it in the sandbox, prove tests green, CVE gone, test files unchanged | Opus design, DeepSeek build | Merge rate on 20 real red upgrade PRs recorded |
| C5 | **GitLab self-managed.** Commit status, external status checks on Ultimate, MR notes | Sonnet | Contract tests against a recorded API |
| C6 | **Bug to repro test to fix job** | Opus | Red-first bench numbers |
| C7 | **Webhook mode** next to polling, for latency | Sonnet | Component test |

## Stage D: the regulated upsell (after the first design partners)

| WP | What |
|---|---|
| D1 | Signed change record per merge and an offline verifier (`heimdall verify`) |
| D2 | Evidence export to Vanta and Drata; break-glass and bypass report |
| D3 | Retention, legal hold, export per year, offboarding |
| D4 | Threat model, Go fuzzing on parsers, sandbox escape tests in CI, SBOM and signed releases |
| D5 | GitHub Enterprise Server and Bitbucket Data Center on request |

## Go-to-market beside the build

| Step | When |
|---|---|
| Five calls with platform leads mid agent rollout; offer the $5k 30-day pilot | week 2 |
| Landing page with measured numbers only (drop count, p95, cost, bench recall) | after A0 and A3 |
| Show HN: "we drop AI review comments that cannot prove themselves, and print the count" | after A7 |
| Open-source `heimdall memory` launch | after B4 |
| Three paid design partners ($5k for 90 days) | after A7 |
