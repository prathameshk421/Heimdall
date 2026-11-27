# Claims on site/index.html (v9)

v9 (27 November 2026) rebuilds the page from the v8 product page, kept as `site/archive/previous-v8.html`. Every
number and source link is the v8 one; no new number was added. Changes that carry meaning:

| Item | Source |
|---|---|
| Hero lede and rule line: fails twice on the PR, passes on the fix; a person approves the exact commit | `docs/product/STRATEGY.md:20-21,36-38` |
| Hero fact: 0 false blocks on clean changes in every live Proof Bench run | Same as v8 Table 3 (`docs/sota/BENCH.md`) |
| Hero scene (12 comments, 1 proven, 11 hidden) | Illustration, same comments as v8 Figure 1. Labelled in the scene's code and by the lab's Figure 1 note |
| Proof lab (Figure 1): verdicts per comment, test output, commit `e4f5a6b`, the push, approval and merge | Illustration. The verdict rules come from `docs/product/STRATEGY.md:18-21` and `docs/sota/BENCH.md:423` (a build error is not proof). F1 text, file, lines, test and `a1b2c3d` from the summary golden as in v8 |
| "Every approval on a1b2c3d is void" after a new push | An approval counts only on the head commit (README, v8 section 7) |
| 70 / 70 sandbox probes | Shown once, in "Built to be trusted" (v8 showed it in two places) |

# Claims on site/index.html (v7)

Every claim on the landing page, with its source file and line. Paths are relative to the repo root on
`product-build`. The page links each number to the same place on
`https://github.com/prathameshk421/Heimdall/blob/product-build/`. If a source changes, update the page and this table
in the same commit. The v5 page is kept as `site/archive/previous-v6.html` (and v4 as `site/archive/previous-v5.html`). The v6 page
reorders the sections and puts the numbers in tables; the claims below map to its sections as noted.


v7 (`site-v7`) changes only color, texture and art. No text moved, so every row below still holds. The v6 page is
kept as `site/archive/previous-v7.html`. Two new items carry words or meaning:

| Item | Source |
|---|---|
| Hero art alt text: two columns, light through the gap draws a check, a figure walks toward the gate | Illustration of the Gateposts mark (`site/DESIGN.md` section 5). It makes no product claim |
| og:image `site/shots/og-v7.png`: title, and "A proven finding blocks the merge. Unproven ones are counted." | Same as the lede and the og:description: `docs/product/STRATEGY.md:13-15,36-38` |

## 1. Title, lede and Figure 1

| Claim | Source |
|---|---|
| Lede: latest live run proved 1 of 3 scored bug cases, 0 false positives on 3 clean cases | `docs/sota/BENCH.md:815-816` |
| Lede: a provider refusal ended 7 cases; cause known; fix in progress | `docs/sota/BENCH.md:784-793` (rows), `docs/sota/BENCH.md:836-843`, `docs/product/TRACKER.md:72` |

Figure 1 (the demo, ex hero):

| Claim | Source |
|---|---|
| A proven finding blocks the merge; unproven ones are not shown and are counted | `docs/product/STRATEGY.md:13-15` |
| A person approves the exact commit | `docs/product/STRATEGY.md:36-38` |
| The GitHub Action is in preview (not shipped) | `docs/product/ROADMAP.md:28`, `docs/product/TRACKER.md:60` |
| F1 text, file and lines `internal/pay/refund.go:42-44`, test `TestRefundRunsOnceWhenRetried`, raised by CodeRabbit, commit `a1b2c3d` | `internal/domain/summary/testdata/enforce_one_e1_bug.golden.md:1,3,9-10` |
| "Fails twice on the PR, passes on the fix" is the proof rule | `docs/product/STRATEGY.md:20-21` |
| The 11 other comments, their wording, the PR title and the count of 11 | Illustration, no source. Labelled "Illustration" under the demo. 11 sits near the t3code median of 10 per PR (`docs/product/research/13-t3code-bot-comments.md:70`). One line reuses the golden's F2 text |
| Merge locked until a person approves this exact commit | `docs/product/STRATEGY.md:36-38`, `docs/product/design/ACTION_MODE.md:380` |

## 2. The problem (section 1, Table 1)

| Claim | Source |
|---|---|
| 411 bot comments | `docs/product/research/13-t3code-bot-comments.md:9` |
| 30 merged PRs of pingdotgg/t3code | `docs/product/research/13-t3code-bot-comments.md:10,14` |
| Median 10, maximum 30 comments per PR | `docs/product/research/13-t3code-bot-comments.md:70` |
| CodeRabbit 48%, 29 of 60 | `docs/product/research/13-t3code-bot-comments.md:54` |
| Macroscope 43%, 75 of 175 | `docs/product/research/13-t3code-bot-comments.md:52` |
| Bugbot (cursor[bot]) 24%, 32 of 135 | `docs/product/research/13-t3code-bot-comments.md:53` |
| Table 1 bars: CodeRabbit 60, Macroscope 175, Bugbot 135 comments in all | `docs/product/research/13-t3code-bot-comments.md:52-54` |
| Bugbot 70% dismissed, 95 of 135 | `docs/product/research/13-t3code-bot-comments.md:53,63` |
| "Changed" label definition; the label is a proxy; a nearby fix can be missed | `docs/product/research/13-t3code-bot-comments.md:30-32,40-41` |

Bar widths are the counts divided by each bot's total (for example 95 / 135 = 70.37%).

## 3. How it works (section 2, Figure 2)

| Claim | Source |
|---|---|
| Heimdall reads its own and other bots' findings | `docs/product/STRATEGY.md:13-15` |
| Fails twice on the PR head, passes on the fixed tree; one red run is not proof | `docs/product/STRATEGY.md:20-21` |
| A build error is not proof | `docs/sota/BENCH.md:423`, `docs/product/TRACKER.md:53` |
| Proven block, unproven counted, a person approves the exact commit | `docs/product/STRATEGY.md:36-38` |
| Before list (3 CodeRabbit and 1 Greptile comment) and the After summary text | `internal/domain/summary/testdata/enforce_one_e1_bug.golden.md:3-43` (F1 to F4 texts, the unproven block, the closing line) |

## 4. Built to be trusted (section 4)

| Claim | Source |
|---|---|
| 70 escape probes blocked: 34 fetch and 36 exec, 0 unknown, GitHub-hosted runner, run 44910812111 | `docs/product/design/ACTION_MODE_SPIKE.md:326,541`, `docs/product/TRACKER.md:49` |
| No model in the gate; a model may propose, only proof decides | `docs/product/STRATEGY.md:104-105` |
| A missing fact is "unknown", never a pass | `docs/product/design/ACTION_MODE.md:428`, `internal/domain/fastcheck/doc.go` (Invariants) |
| Runs on your own VM; the Action for your CI is in preview | `docs/product/STRATEGY.md:108-109`, `README.md:73`, `docs/product/ROADMAP.md:28` |
| Any OpenAI-compatible endpoint (Ollama, vLLM, LiteLLM, DeepSeek, a company gateway); no key for a local endpoint | `internal/adapters/openrouter/doc.go:4-10` |
| Log on one git ref with an HMAC over each chain head; a bad MAC fails closed | `docs/product/design/ACTION_MODE.md:26,196`, `docs/product/TRACKER.md:39` |
| Not verified: load stress, fork bomb, non-Go package managers, self-hosted and ARM runners | `docs/product/TRACKER.md:49` |

## 5. What is built (section 5, Table 4)

| Claim | Source |
|---|---|
| Eight fast-check clauses, no model | `internal/domain/fastcheck/fastcheck.go:13-20`, `docs/product/STRATEGY.md:66-67` |
| 383 of 383 t3code comments parse, from seven reviewers | `docs/product/TRACKER.md:63` |
| Pinned tests read-only; weaker asserts and mock-of-subject flagged | `docs/product/TRACKER.md:53` |
| Worst-case cost bound before every model call; retries inside the cap | `docs/product/TRACKER.md:60,70` |
| Test selection: 106.7 s to 13.1 s over 5 commits | `docs/sota/BENCH.md:145`, `docs/product/TRACKER.md:55` |
| Cost guard held in run 3: stopped at $2.90 of $3.00 | `docs/sota/BENCH.md:800` |
| Shadow mode: two weeks of "would block because" | `docs/product/STRATEGY.md:72` |
| Merged and tested; the Action wiring (AM15) is next; no customer runs it | `docs/product/TRACKER.md:60`, `docs/product/STRATEGY.md:115-120` |

## 6. Results so far (section 3, Tables 2 and 3)

| Claim | Source |
|---|---|
| Valid case: test red before the fix, green after; each verdict held on two runs | `docs/sota/BENCH.md:28-30` |
| Go: 7 valid bug, 3 valid clean | `docs/sota/BENCH.md:64` |
| TypeScript: 10 valid bug, 2 valid clean | `docs/sota/BENCH.md:623` |
| Rust: 1 valid bug | `docs/sota/BENCH.md:624` |
| Payment fixture: 1 valid bug (`fixture-refunds`, valid since P2.1g), 4 valid clean | `docs/sota/BENCH.md:541-545`, `docs/sota/BENCH.md:421-427` |
| 28 valid cases in all | `docs/sota/BENCH.md:831` (7+10+1+1 bug, 3+2+4 clean) |
| Fixture repo `aarush323/heimdall-fixture-pay`, owned by the team, planted bugs | `docs/sota/BENCH.md:401-403` |
| Model `deepseek-ai/deepseek-v4.1-flash-1017` on Deploy; cap $3.00 a run, $0.50 a case | `docs/sota/BENCH.md:343-352,708-717,768-780` |
| Run 1: 10 cases, 9 scored, 0 proposed, recall 0 of 7, FP 0 of 2, $0.86 | `docs/sota/BENCH.md:371-376,382-386` |
| Run 1 cause: loop ended on the first text answer; fixed | `docs/sota/BENCH.md:380-397` |
| Run 2: 18 cases, 2 scored, 2 proposed, 0 proven, FP 0 of 1, $1.18 | `docs/sota/BENCH.md:745-749,756-758` |
| Run 2 recall shown as "–": the file records 0 of 1, one bug case only | `docs/sota/BENCH.md:744` |
| Run 2: real tool call every recorded turn; 15 calls in 14 of 18 cases HTTP 500; retries added | `docs/sota/BENCH.md:755-766` |
| Run 3: 28 cases, 6 scored, 5 proposed, 2 proven, recall 1 of 3, FP 0 of 3, $2.92 | `docs/sota/BENCH.md:815-822,831` |
| Run 3: `fixture-refunds` proposed 2, proven 2, hit; first proven hit on a live run | `docs/sota/BENCH.md:794,831-833` |
| Run 3: `echo-1799` and `logrus-1564` missed; injection case 3 proposed, 0 proven, not injected | `docs/sota/BENCH.md:833-834` |
| Run 3: 22 of 28 unscored: 7 HTTP 500 after 3 attempts, 3 per-case budget, 2 stopped by the cap mid-case, 10 not run; cap reached after 33 min | `docs/sota/BENCH.md:784-811,831` |
| Run 3 cause: `reasoning_content` not sent back in thinking mode with tools; fix AM12d in progress, not verified | `docs/sota/BENCH.md:838-843`, `docs/product/TRACKER.md:72` |
| All runs priced at $0.50/M input (cached too) and $2.00/M output; run 3 had 91% cache reads; real bill likely lower, not verified | `docs/sota/BENCH.md:350,715,775,845-847` |
| No recall rate claimed: 3 scored bug cases is too few | Our reading of `docs/sota/BENCH.md:815` |

Note on the HTTP 500 count: the run 3 diagnosis prose (`docs/sota/BENCH.md:836`) says nine cases. The case table
(`docs/sota/BENCH.md:784-793`) and `docs/sota/bench-live-3-2026-11-21.json` show seven cases whose error is an HTTP
500. The page uses seven and says so in note 6. The bench file should be corrected on `product-build`.

## 7. Questions (section 6)

| Claim | Source |
|---|---|
| Referees other bots and its own findings with one rule | `docs/product/STRATEGY.md:104-105` |
| Unproven means not shown, not false; a person can open the list | `docs/product/STRATEGY.md:18-19` |
| Break-glass: reason, named person, recorded, open until another approver reviews, never from an agent | `docs/product/design/ACTION_MODE.md:428-431`, `docs/product/STRATEGY.md:73`, `docs/product/TRACKER.md:45` |
| Break-glass record built; the command ships with the Action | `docs/product/TRACKER.md:45`, `docs/product/ROADMAP.md:29` |
| Worst-case cost guard before every call | `docs/product/TRACKER.md:60` |
| Tests run with no network; installs reach only an allowlisted proxy | `docs/product/design/ACTION_MODE_SPIKE.md:572-576` |

## 8. What comes next (section 7, Figure 3)

| Claim | Source |
|---|---|
| Next: AM12d, then live run 4 on the unfinished cases | `docs/product/TRACKER.md:72` |

Figure 3 and the Action:


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
- A recall rate from a real model. Run 3 scored 3 bug cases, too few for a rate.
- The 9 cases the brief and the run 3 diagnosis name. The case table supports 7 (see section 6).
- Customer logos, quotes or install counts. None exist.
