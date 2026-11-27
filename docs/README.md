# Heimdall docs

Start with the top row of the table that fits you.

The product page is at **https://heimdall-sage.vercel.app**. It covers what Heimdall does, the proof rule in one worked
example, the integrations, and a two-minute demo.

## Use it

| Doc | What's in it |
|---|---|
| [SETUP.md](SETUP.md) | **Start here.** Develop locally, or run Heimdall on your own VM, step by step |
| [CLI.md](CLI.md) | Every `heimdall` command, every chat command, the main Make targets |
| [RUNBOOK.md](RUNBOOK.md) | Switch on the VM, tunnels, status, logs, reboot, recovery, known limits |
| [COMPANY_SETUP.md](COMPANY_SETUP.md) | Phase-by-phase checklist for a company rollout, with what is and is not verified |
| [MATTERMOST_ACCESS.md](MATTERMOST_ACCESS.md) | Giving the team Mattermost through a tunnel |
| [TEAMS_SETUP.md](TEAMS_SETUP.md) | Microsoft Teams admin guide |
| [../deploy/agents/README.md](../deploy/agents/README.md) | Installing the OpenCode agents and the Switch controller |
| [../deploy/cards/README.md](../deploy/cards/README.md) | Personal context cards for reviewers |
| [../deploy/RELEASES.md](../deploy/RELEASES.md) | Signed releases and deploy verification |

## Understand it

| Doc | What's in it |
|---|---|
| [PRODUCT_PITCH.md](PRODUCT_PITCH.md) | The problem and the pitch |
| [HEIMDALL_MVP.md](HEIMDALL_MVP.md) | What the MVP does |
| [TECH_SPEC.md](TECH_SPEC.md) | The technical spec: state machine, evidence, gate, sandbox, commands |
| [TECH_DESIGN.md](TECH_DESIGN.md) | The first rough design |
| [BLOG_POST.md](blog/BLOG_POST.md) | How we built it, and why |

## Build it

| Doc | What's in it |
|---|---|
| [../CONTRIBUTING.md](../CONTRIBUTING.md) | **Start here to contribute.** Setup, checks, rules, PR checklist |
| [../SECURITY.md](../SECURITY.md) | How to report a vulnerability |
| [../AGENTS.md](../AGENTS.md) | Rules for coding agents (also `CLAUDE.md`) |
| [CODING_STANDARDS.md](CODING_STANDARDS.md) | Code rules |
| [TEST_STRATEGY.md](TEST_STRATEGY.md) | Test layers and budget |
| [MVP_SCENARIOS.md](MVP_SCENARIOS.md) | The scenario board: the prototype is the test |
| [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md) | Task list with acceptance criteria |
| [TRACKER.md](TRACKER.md) | Build tracker with evidence per task |
| [PROGRESS.md](PROGRESS.md) | Current state, live evidence, what is not verified |
| [harness/](harness) | Hooks, settings and templates for agent sessions |

## Plan the next stage

Architecture plans written Nov 14, 2026. Nothing in them is built yet.

| Doc | What's in it |
|---|---|
| [AGENT_INTERACTION_ARCH.md](AGENT_INTERACTION_ARCH.md) | **Read first.** The construction rules and foundations for all three plans: facts and views instead of a state machine, the kind table, seats, the two standing channels with a thread per change, how `@` reaches each agent. Agents talk to each other and to people through Switch; OpenCode supplies skills and subagents; the server keeps tools, evidence and rules |
| [REVIEW_ARCH.md](REVIEW_ARCH.md) | One review pipeline for any language: checks as profile rows with SARIF out, seats with lens cards and stack skills, a refute step, scope and budget, co-change mining, the path to proving context helps |
| [TEST_AGENT_ARCH.md](TEST_AGENT_ARCH.md) | Tests as evidence in any language: the test plan, test kinds and lints from the profile, mutants and the survivor loop, counter-implementations, UI tests with Playwright (its runner, its MCP server, its planner and generator) and the browser inside the sandbox |
| [BUILD_PLAN.md](BUILD_PLAN.md) | How the three plans get built: seven modules, each built whole by one agent, with the folders it owns, its steps in order, the tests and golden files it must satisfy, the model tier it needs, and the hard parts a strong model must write or review |
| [CONTEXT_CAPTURE_PLAN.md](CONTEXT_CAPTURE_PLAN.md), [KNOWLEDGE_TOPOLOGY.md](KNOWLEDGE_TOPOLOGY.md), [REVIEW_MOAT.md](REVIEW_MOAT.md) | Capturing a developer's and a repository's knowledge, where it lives, and what was measured |

## Demo

| Doc | What's in it |
|---|---|
| [DEMO.md](archive/hackathon/DEMO.md), [DEMO_SCRIPT.md](archive/hackathon/DEMO_SCRIPT.md), [DEMO_RUN.md](archive/hackathon/DEMO_RUN.md) | Demo script, shot list and run sheet |
| [VIDEO_PLAN.md](archive/hackathon/VIDEO_PLAN.md), [VIDEO_DESIGN.md](archive/hackathon/VIDEO_DESIGN.md) | The demo video |

## History

Written during the build and kept as a record: [BRAINSTORM_BACKLOG.md](BRAINSTORM_BACKLOG.md) (decisions D1 to D16x), [STAGE_1_FIXES.md](STAGE_1_FIXES.md), [HANDOFF.md](archive/hackathon/HANDOFF.md), [DEEPSEEK_PLAN.md](archive/hackathon/DEEPSEEK_PLAN.md), [WIRING_OWNER_STEPS.md](archive/hackathon/WIRING_OWNER_STEPS.md), [BLOG_PLAN.md](blog/BLOG_PLAN.md).
