# Heimdall: raw idea backlog (before the critic)

Status: draft, Nov 21 2026. Every idea here goes to the critic pass (`CRITIC.md`). Ideas that survive go to `ROADMAP.md`.
Sources are the research files in `research/`. A tag like `[01 §6]` points to a file and a section.

Score key: Pain (1 to 5), Pay (willingness to pay, 1 to 5), Fit (fit with the built product, 1 to 5), Cost (build effort:
S under 3 days, M under 2 weeks, L more). "Coherent" asks one question: does it make the core promise stronger?

The core promise, in one line: **nothing merges without proof, and the proof is on record.**

## A. The core promise (make it sharper, not wider)

| # | Idea | Why (evidence) | Pain | Pay | Fit | Cost | Coherent |
|---|---|---|---|---|---|---|---|
| A1 | "Dropped for no evidence" counter on every PR and the board | Top complaint is speculative noise [02 T1 #5, #6]. Show the noise we saved | 5 | 3 | 5 | S | yes |
| A2 | One-page proof summary per PR (what was proven, what was not, what a person must judge) | Review cannot keep up [02 rank 1]. A reviewer reads one page, not 1,400 lines | 5 | 4 | 5 | M | yes |
| A3 | Approval bound to the exact SHA, voided on push, shown in the check summary | Nobody sells it [01 §6.3]. Already built; make it visible | 4 | 4 | 5 | S | yes |
| A4 | Test tamper guard: the code author (person or agent) cannot edit or delete the pinned red tests | Agents delete tests [02 T3 #3] | 4 | 3 | 5 | S | yes |
| A5 | Mock-of-subject detector (a test that mocks the module under test is rejected) | 36% vs 26% mock rate [02 T3 #2] | 3 | 3 | 5 | S | yes |
| A6 | Mutation score on changed lines as E2 evidence (go-mutesting, Stryker, mutmut, PIT) | "100% coverage, 4% mutation score" [02 T3 #2]; Meta ACH [02 T3 #7] | 3 | 3 | 4 | M | yes |
| A7 | Diff coverage gate from cover profiles | Gap for small teams [03 §9] | 3 | 3 | 4 | S | yes |
| A8 | Severity is decided by the evidence tier, never by the model | "LLMs cannot filter nits" (Greptile founder) [02 T1 #3, #4] | 4 | 3 | 5 | S | yes |
| A9 | Signed, hash-chained change record per merge plus an offline verifier CLI | Audit trail gap [02 rank 3], CC8.1, PCI 6.2.3.1 [05 §3] | 3 | 5 | 5 | M | yes |
| A10 | Control-mapped audit pack export (SOC 2 CC8.1, ISO 27001 A.8.32, PCI 6.5.1, DORA) | Regulated buyer has budget and a deadline [05 §6] | 3 | 5 | 4 | M | yes |
| A11 | Provenance: mark which lines an agent wrote (git trailers, Co-authored-by, agent session ids) and require stronger evidence for them | "Who approved this AI code" [02 T7] | 3 | 4 | 4 | M | yes |
| A12 | Printed cost per PR in the check summary | Buyers hate surprise bills [02 rank 7] | 3 | 3 | 5 | S | yes |

## B. Adoption (remove every reason a team says no)

| # | Idea | Why | Pain | Pay | Fit | Cost | Coherent |
|---|---|---|---|---|---|---|---|
| B1 | GitHub App with manifest flow (replace PATs) | Rulesets bypass, pinned required check, merge queue [03 #1] | 5 | 3 | 4 | M | yes |
| B2 | Ticketless PR as the default path; Jira becomes optional context | Most teams start at the PR, not the ticket. Jira-first blocks trials | 5 | 3 | 4 | M | yes |
| B3 | Switch becomes optional: a native Slack app (Socket Mode) and a generic webhook notifier | Switch is a hackathon dependency no buyer has [inventory] | 5 | 3 | 3 | M | yes |
| B4 | `heimdall init`: one command that writes config, makes the GitHub App, checks the model key, opens the first PR | Install has many manual steps [inventory] | 5 | 3 | 5 | M | yes |
| B5 | Lite profile: one binary plus Postgres (or embedded SQLite), agents as short jobs, no resident agent units, Graphiti off | 9 containers and 7 agent units on a 2 vCPU VM [README] | 4 | 3 | 4 | L | yes |
| B6 | One OpenAI-compatible model port (Ollama, vLLM, llama.cpp, LiteLLM) | Air-gapped buyers, BYO key [03 §6] | 4 | 4 | 4 | S | yes |
| B7 | GitHub merge queue support (`merge_group`) | Without it, queue users cannot adopt [03 #2] | 3 | 3 | 4 | S | yes |
| B8 | GitHub Action mode (zero infra): run the gate on the customer's own runner | The trial path with no VM; feeds the server sale | 4 | 2 | 3 | M | maybe |
| B9 | GitLab self-managed adapter (commit status, external status checks on Ultimate) | Self-managed GitLab is the company-VM crowd [03 #8] | 4 | 4 | 3 | L | yes |
| B10 | Forgejo/Gitea adapter | Self-hosting crowd, weakly served [01 §6 risks] | 2 | 2 | 3 | M | yes |
| B11 | Linear and GitHub Issues behind a `WorkItem` port | Linear growth [03 #10] | 3 | 2 | 4 | M | yes |
| B12 | Shadow mode: comment-only for two weeks, then report "what Heimdall would have blocked" | Trust before gating. Removes the fear of a blocking tool | 4 | 3 | 5 | S | yes |
| B13 | Language packs for the sandbox: Go, Python, Node/TS, Java, Rust profiles with cached toolchains | A Go-only runner sells to few | 5 | 3 | 4 | M | yes |

## C. Lightweight and cheap (the "cheaper than all" claim must be measured)

| # | Idea | Why | Cost |
|---|---|---|---|
| C1 | Skip the LLM on trivial PRs (docs, lockfiles, generated code, under N lines) by deterministic rules | Bugbot users: "70-80% of our PRs are tiny" [02 T1 #9] | S |
| C2 | Prompt cache by design: stable prefix (skill, repo map), variable suffix (diff). Measure cache hit rate | DeepSeek cache read is about 7x cheaper [03 §6] | S |
| C3 | Per-PR token budget with a hard cap and a "budget hit" status | Predictable cost [02 rank 7] | S |
| C4 | Deterministic analyzers first (SARIF), LLM only on what they cannot see | E2 evidence is free and exact | M |
| C5 | Agents as on-demand processes, scale to zero | Idle memory of resident agents | L |
| C6 | Model routing: flash model for triage and refute, stronger model only for proof writing | Spend where it changes the result | M |
| C7 | Publish "cost per merged PR" and "RAM idle / under load" on the landing page with the bench that produced them | Claim must be real (R9) | S |

## D. Security of Heimdall itself (sold to security buyers, so it must be clean)

| # | Idea | Cost |
|---|---|---|
| D1 | Threat model doc plus an external-style review: prompt injection from PR text, diff, ticket, check output | M |
| D2 | Least-privilege GitHub App permissions; no write to contents unless a fix is approved | S |
| D3 | SBOM, signed releases (cosign), SLSA provenance for Heimdall's own builds | S |
| D4 | Sandbox hardening proof: seccomp profile, no network, read-only, resource caps, escape tests in CI | M |
| D5 | Secrets never reach agents: redaction on every input, egress allowlist for the agent process | M |
| D6 | Fuzz the parsers (SARIF, chat commands, webhook payloads) with Go native fuzzing | S |

## E. Reach (later, only if it serves the core)

| # | Idea | Verdict before the critic |
|---|---|---|
| E1 | Heimdall MCP server so coding agents can ask "what proof is missing on my PR" and fix it before a human looks | strong: closes the loop for agent authors |
| E2 | AGENTS.md / CLAUDE.md aware reviews (repo rules become rule cards) | strong: cheap and common |
| E3 | Personal context packs from the developer's laptop | keep, but it is a later moat, not the wedge |
| E4 | OSS maintainer mode (slop firewall as a free Action) | brand only; low pay [02 rank 9] |
| E5 | Teams bot | only on buyer request [03 §5] |
| E6 | Web board as the main UI (proof summary, gate reasons, audit export) | needed for the regulated buyer |
| E7 | Auto-fix PRs | reject: crowded, against "we do not write the code" |
| E8 | Our own LLM leaderboard from blog numbers | reject: R9 |

## F. Proof that the product works (testing on real projects)

| # | Idea |
|---|---|
| F1 | Heimdall Proof Bench: replay bug-introducing PRs from real OSS repos (SZZ) and clean merged PRs; score recall, precision, evidence rate and cost |
| F2 | Red-first bench: issue plus fix pairs; the test must fail before the fix and pass after (SWT-bench style) |
| F3 | Nightly run on a fixed set of 20 repos in 5 languages, results in `docs/sota/RUNS.md` and on the site |
| F4 | Dogfood: Heimdall gates its own repo on every PR |

## G. Beyond the merge gate: what we would sell to make money (owner ask, Nov 21)

Thesis under test (`research/08`): the asset is not the gate. It is a machine that proves agent work on the company's
own VM: a no-network sandbox, red-first tests, evidence tiers, a deterministic gate, a person on the exact commit, and
cheap models. Buyers pay for agent jobs whose result a machine can check. The merge gate is the trust layer every job
passes through, for human PRs and agent PRs alike.

One product line: **Heimdall, the company's own agents that only hand over work with proof.**

| # | Job (each ends as a PR through the gate) | Machine-checkable proof | First guess |
|---|---|---|---|
| G1 | Fix broken dependency and CVE upgrade PRs (Dependabot/Renovate backlog) | Build and tests green, scanner shows the CVE gone | strong wedge candidate: backlog pain is common, outcome is binary |
| G2 | CI failure triage, flaky test detection and fix | N reruns green, flake rate before and after | strong: CI pain [02 rank 8], cheap to verify |
| G3 | Missing-test backfill on changed or legacy code | Mutation score and diff coverage up, tests fail on a planted mutant | reuses the test agent directly |
| G4 | Version and framework migrations | Build and tests green on the new version | crowded (Moderne, OpenRewrite); later |
| G5 | Evidence-backed PR review and merge gate (today's product) | E1/E2/E3 per finding | the trust layer |
| G6 | Incident to repro test: from a stack trace or bug report, write the failing test first | The test fails on main | strong demo, fits red-first |
| G7 | Security finding autofix (SARIF in, fix PR out) | Scanner clean and tests green | competes with Copilot Autofix, Snyk |
| G8 | Lint and type debt burn-down at night | Lint count down, tests green | cheap, boring, sellable to platform teams |
| G9 | Sell the sandbox runtime itself (rootless, no network, signed results) to other agent builders | Escape tests, provenance | different buyer; probably a distraction |

Ways to charge that fit outcome jobs: per merged proof-carrying PR, per repo per month, or per VM licence. To test in
`research/08` against Snyk/Mend autofix, Moderne, Devin, Factory and Copilot coding agent prices.
