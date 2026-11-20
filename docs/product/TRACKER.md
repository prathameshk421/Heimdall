# Product track: who does what

Coordinator: session "Make Heimdall a Sellable Product" (Opus). Stage 0 (SOTA quality plan) runs in the other session on
`heimdall-build`; this track writes only `docs/product/` there and merges delegate branches after review.

| Item | Owner (provider / model) | Branch or file | Status | Evidence |
|---|---|---|---|---|
| Research 01 competitors | Claude / Sonnet | `research/01-competitors.md` | done | 30 searches, 5 vendor pages read |
| Research 02 pain points | Claude / Sonnet | `research/02-pain-points.md` | done | Reddit and X blocked, see 06 |
| Research 03 integrations | Claude / Sonnet | `research/03-integrations.md` | done | 32 searches, no page fetches |
| Research 04 eval and real repos | Claude / Sonnet | `research/04-eval-and-real-repos.md` | done | 8 Go suites timed on this Mac |
| Research 05 market and GTM | Claude / Sonnet | `research/05-market-gtm.md` | done | 35 searches |
| Research 06 Reddit and X | Claude Personal / Sonnet | `research/06-reddit-x.md` | done | Reddit blocked by every method; about 60 HN comments via Algolia, about 25 X posts via search |
| Code inventory | Claude / Sonnet (Explore) | summarized in `STRATEGY.md` §3 | done | file:line pointers |
| Critic 1 | OpenCode / DeepSeek V4.1 Flash | `CRITIC-deepseek.md` | running | |
| Critic 2 | Grok 4.7 | `CRITIC-grok.md` | done | wedge: proof or silence on the PR, shadow mode, Action first |
| P1.7 any OpenAI-compatible endpoint | Claude Personal / Sonnet | `p1.7-model-endpoint` | merged `6237729` | make fast exit 0, 3440 tests; make verify and live model not verified; http to a named LAN host (ollama:11434) still needs https, follow-up |
| P2.1a Proof Bench dataset and validator | Claude Personal / Opus | `p2.1-proof-bench` | running | |
