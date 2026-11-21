# Claims on site/index.html (v5)

Every claim on the landing page, with its source file and line. Paths are relative to the repo root on
`product-build`. The page links each number to the same place on
`https://github.com/prathameshk421/Heimdall/blob/product-build/`. If a source changes, update the page and this table
in the same commit. The v4 page and its claims are kept as `site/previous-v5.html`.

## 1. Hero and demo

| Claim | Source |
|---|---|
| A proven finding blocks the merge; unproven ones are not shown and are counted | `docs/product/STRATEGY.md:13-15` |
| A person approves the exact commit | `docs/product/STRATEGY.md:36-38` |
| The GitHub Action is in preview (not shipped) | `docs/product/ROADMAP.md:28`, `docs/product/TRACKER.md:60` |
| F1 text, file and lines `internal/pay/refund.go:42-44`, test `TestRefundRunsOnceWhenRetried`, raised by CodeRabbit, commit `a1b2c3d` | `internal/domain/summary/testdata/enforce_one_e1_bug.golden.md:1,3,9-10` |
| "Fails twice on the PR, passes on the fix" is the proof rule | `docs/product/STRATEGY.md:20-21` |
| The 11 other comments, their wording, the PR title and the count of 11 | Illustration, no source. Labelled "Illustration" under the demo. 11 sits near the t3code median of 10 per PR (`docs/product/research/13-t3code-bot-comments.md:70`). One line reuses the golden's F2 text |
| Merge locked until a person approves this exact commit | `docs/product/STRATEGY.md:36-38`, `docs/product/design/ACTION_MODE.md:380` |

## 2. The problem

| Claim | Source |
|---|---|
| 411 bot comments | `docs/product/research/13-t3code-bot-comments.md:9` |
| 30 merged PRs of pingdotgg/t3code | `docs/product/research/13-t3code-bot-comments.md:10,14` |
| Median 10, maximum 30 comments per PR | `docs/product/research/13-t3code-bot-comments.md:70` |
| CodeRabbit 48%, 29 of 60 | `docs/product/research/13-t3code-bot-comments.md:54` |
| Macroscope 43%, 75 of 175 | `docs/product/research/13-t3code-bot-comments.md:52` |
| Bugbot (cursor[bot]) 24%, 32 of 135 | `docs/product/research/13-t3code-bot-comments.md:53` |
| Bugbot 70% dismissed, 95 of 135 | `docs/product/research/13-t3code-bot-comments.md:53,63` |
| "Changed" label definition; the label is a proxy; a nearby fix can be missed | `docs/product/research/13-t3code-bot-comments.md:30-32,40-41` |

Bar widths are the counts divided by each bot's total (for example 95 / 135 = 70.37%).

## 3. How it works

| Claim | Source |
|---|---|
| Heimdall reads its own and other bots' findings | `docs/product/STRATEGY.md:13-15` |
| Fails twice on the PR head, passes on the fixed tree; one red run is not proof | `docs/product/STRATEGY.md:20-21` |
| A build error is not proof | `docs/sota/BENCH.md:423`, `docs/product/TRACKER.md:53` |
| Proven block, unproven counted, a person approves the exact commit | `docs/product/STRATEGY.md:36-38` |
| Before list (3 CodeRabbit and 1 Greptile comment) and the After summary text | `internal/domain/summary/testdata/enforce_one_e1_bug.golden.md:3-43` (F1 to F4 texts, the unproven block, the closing line) |

## 4. Built to be trusted

| Claim | Source |
|---|---|
| 70 escape probes blocked: 34 fetch and 36 exec, 0 unknown, GitHub-hosted runner, run 44910812111 | `docs/product/design/ACTION_MODE_SPIKE.md:326,541`, `docs/product/TRACKER.md:49` |
| No model in the gate; a model may propose, only proof decides | `docs/product/STRATEGY.md:104-105` |
| A missing fact is "unknown", never a pass | `docs/product/design/ACTION_MODE.md:428`, `internal/domain/fastcheck/doc.go` (Invariants) |
| Runs on your own VM; the Action for your CI is in preview | `docs/product/STRATEGY.md:108-109`, `README.md:73`, `docs/product/ROADMAP.md:28` |
| Any OpenAI-compatible endpoint (Ollama, vLLM, LiteLLM, DeepSeek, a company gateway); no key for a local endpoint | `internal/adapters/openrouter/doc.go:4-10` |
| Log on one git ref with an HMAC over each chain head; a bad MAC fails closed | `docs/product/design/ACTION_MODE.md:26,196`, `docs/product/TRACKER.md:39` |
| Not verified: load stress, fork bomb, non-Go package managers, self-hosted and ARM runners | `docs/product/TRACKER.md:49` |

## 5. What is built

| Claim | Source |
|---|---|
| Eight fast-check clauses, no model | `internal/domain/fastcheck/fastcheck.go:13-20`, `docs/product/STRATEGY.md:66-67` |
| 383 of 383 t3code comments parse, from seven reviewers | `docs/product/TRACKER.md:63` |
| Pinned tests read-only; weaker asserts and mock-of-subject flagged | `docs/product/TRACKER.md:53` |
| Worst-case cost bound before every model call; retries inside the cap | `docs/product/TRACKER.md:60,70` |
| Test selection: 106.7 s to 13.1 s over 5 commits | `docs/sota/BENCH.md:145`, `docs/product/TRACKER.md:55` |
| Shadow mode: two weeks of "would block because" | `docs/product/STRATEGY.md:72` |
| Merged and tested; the Action wiring (AM15) is next; no customer runs it | `docs/product/TRACKER.md:60`, `docs/product/STRATEGY.md:115-120` |

## 6. Honest scorecard

| Claim | Source |
|---|---|
| Go: 7 valid bug, 3 valid clean | `docs/sota/BENCH.md:64` |
| TypeScript: 10 valid bug, 2 valid clean | `docs/sota/BENCH.md:623` |
| Rust: 1 valid bug | `docs/sota/BENCH.md:624` |
| Payment fixture: 4 valid (injection scored clean, 3 clean) | `docs/sota/BENCH.md:421-427` |
| Each verdict held on two runs | `docs/sota/BENCH.md:28-30,512,592` |
| Live run 1: 10 cases, 0 findings proposed, recall 0 of 7, false positives 0 of 2, $0.86 | `docs/sota/BENCH.md:343,371-376,382` |
| Run 1 cause: the loop ended on the first text answer; fixed (AM12b) | `docs/sota/BENCH.md:382-386,394-397` |
| Live run 2: 18 cases; real tool call on every recorded turn | `docs/sota/BENCH.md:708-710,755-756` |
| Run 2: 15 calls in 14 of 18 cases failed with HTTP 500; recall not judged | `docs/sota/BENCH.md:760-761` |
| Run 2: injection case proposed 2, both unproven, no false positive | `docs/sota/BENCH.md:756-758` |
| Run 2 cost $1.18 | `docs/sota/BENCH.md:749,766` |
| Retries added (AM12c, merged); run 3 not run yet | `docs/product/TRACKER.md:70` |
| Both runs used DeepSeek V4.1 Flash at high stated prices | `docs/sota/BENCH.md:343,350,391,708` |
| No recall claim | Neither live run produced a recall score |

## 7. FAQ

| Claim | Source |
|---|---|
| Referees other bots and its own findings with one rule | `docs/product/STRATEGY.md:104-105` |
| Unproven means not shown, not false; a person can open the list | `docs/product/STRATEGY.md:18-19` |
| Break-glass: reason, named person, recorded, open until another approver reviews, never from an agent | `docs/product/design/ACTION_MODE.md:428-431`, `docs/product/STRATEGY.md:73`, `docs/product/TRACKER.md:45` |
| Break-glass record built; the command ships with the Action | `docs/product/TRACKER.md:45`, `docs/product/ROADMAP.md:29` |
| Worst-case cost guard before every call | `docs/product/TRACKER.md:60` |
| Tests run with no network; installs reach only an allowlisted proxy | `docs/product/design/ACTION_MODE_SPIKE.md:572-576` |

## 8. Close

| Claim | Source |
|---|---|
| The Action is not shipped (A7 open), labelled PREVIEW | `docs/product/ROADMAP.md:28`, `docs/product/TRACKER.md:60` |
| `pull_request` trigger, `contents: read`, `pull-requests: read` | `docs/product/design/ACTION_MODE.md:236` |
| 15-minute cap | `docs/product/design/ACTION_MODE.md:18` |
| `uses:` pinned by full SHA; model key on one step only | `docs/product/design/ACTION_MODE.md:241,244` |
| Two weeks in shadow mode | `docs/product/STRATEGY.md:72` |
| Action name `heimdall/action` and the secret name `MODEL_KEY` | Placeholder. No source. Labelled as a placeholder on the page |

## Left out (no source found)

- A license name. STRATEGY plans FSL, but the repo has no LICENSE file.
- A measured fast-check p95. Only the target exists.
- Any recall or false-positive rate from a real model. Neither live run produced one.
- Customer logos, quotes or install counts. None exist.
