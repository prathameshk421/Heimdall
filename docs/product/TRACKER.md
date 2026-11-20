# Product track: who does what

Coordinator: session "Make Heimdall a Sellable Product" (Opus). Stage 0 runs in the other session on `heimdall-build`
in the shared checkout. This track works on `product-build` in its own worktree
(`/Volumes/SSD/Projects/Hackathons/flip_switch_hackathon/heimdall-product`) and tells that session before any merge into
`heimdall-build`.

## Research and critics

| Item | Owner (provider / model) | File | Status | Notes |
|---|---|---|---|---|
| 01 competitors | Claude / Sonnet | `research/01-competitors.md` | done | 30 searches, 5 vendor pages read |
| 02 pain points | Claude / Sonnet | `research/02-pain-points.md` | done | Reddit and X blocked |
| 03 integrations | Claude / Sonnet | `research/03-integrations.md` | done | |
| 04 eval and real repos | Claude / Sonnet | `research/04-eval-and-real-repos.md` | done | 8 Go suites timed |
| 05 market and GTM | Claude / Sonnet | `research/05-market-gtm.md` | done | |
| 06 Reddit and X | Claude Personal / Sonnet | `research/06-reddit-x.md` | done | Reddit refused every method; about 60 HN, about 25 X posts |
| 07 X deep | Grok 4.7 | none | failed | Grok usage limit; harness gives Grok no X search tool |
| 08 proof agent jobs | Claude Personal / Opus | `research/08-proof-agent-jobs.md` | done | wedge job: upgrade PRs green with proof, self-hosted git |
| 09 context packs | Claude Personal / Opus | `research/09-context-packs.md` | done | memory as the free door; no manager scores |
| 10 context OSS | OpenCode / DeepSeek V4.1 Flash free | `research/10-context-oss.md` | done | SpecStory, gitleaks, ferret-scan, age, AGENTS.md |
| Critic 1 | OpenCode / DeepSeek V4.1 Flash (paid) | `CRITIC-deepseek.md` | done (run ended with HTTP 400 after the file was written) | Action plus shadow mode first; fast/slow split; break-glass |
| Critic 2 | Grok 4.7 | `CRITIC-grok.md` | done | proof or silence on the PR |
| Critic 3 on draft 2 | OpenCode / DeepSeek free | `CRITIC-v2.md` | next | |

## Build

| WP | Owner | Branch | Status | Evidence |
|---|---|---|---|---|
| P1.7 any OpenAI-compatible endpoint | Claude Personal / Sonnet | `p1.7-model-endpoint` | merged into `heimdall-build` at `6237729` | make fast exit 0, 3440 tests; make verify and live model not verified; http to a named LAN host still refused |
| A0 / P2.1a Proof Bench dataset and validator | Claude Personal / Opus | `p2.1-proof-bench` | merged into `product-build` at `fa6f832` | 7 Go bug and 3 Go clean cases proven real on 2 validate runs (51.7 s warm); make fast exit 0, 3512 tests; make verify fails only on 4 baseline targets; non-Go cases pending a runner. Next: P2.1b reviewer run, after A1 picks the agent loop |
| Critic 3 on draft 2 | OpenCode / DeepSeek free | `CRITIC-v2.md` | running | |
| A1 Action mode design | Claude Personal / Opus | `a1-action-design` | running | design doc only |
| A9 board login and roles | Claude Personal / Opus | `a9-board-auth` | running | |
| B1 OpenCode and Cursor session readers | Claude Personal / Sonnet | `b1-session-readers` | running | |
