# Claims on site/index.html

Every claim on the landing page, with its source file and line. Paths are relative to the repo root on
`product-build`. The page links each number to the same place. If a source changes, update the page and this table
in the same commit.

## 1. Promise

| Claim | Source |
|---|---|
| Every AI-written change gets its proof before a person spends time on it | `docs/product/STRATEGY.md:34` |
| Proven findings block; unproven ones are not shown and are counted | `docs/product/STRATEGY.md:15`, `docs/product/STRATEGY.md:18-19` |
| No model in the gate | `docs/product/STRATEGY.md:14`, `docs/product/STRATEGY.md:104` |
| A person approves the exact commit | `docs/product/STRATEGY.md:37-38` |
| Hero panel text (F1 refund, 3 unproven from 2 reviewers, approval met) is test golden data | `internal/domain/summary/testdata/enforce_one_e1_bug.golden.md:3,9-10,13,34` |

## 2. Problem

| Claim | Source |
|---|---|
| 411 bot comments | `docs/product/research/13-t3code-bot-comments.md:9` |
| 30 merged PRs of pingdotgg/t3code, TypeScript and Rust | `docs/product/research/13-t3code-bot-comments.md:10,14-15` |
| Eight bot logins | `docs/product/research/13-t3code-bot-comments.md:47` |
| Median 10, maximum 30 top-level comments per PR | `docs/product/research/13-t3code-bot-comments.md:70` |
| 383 top-level comments; 140 acted (37%) | `docs/product/research/13-t3code-bot-comments.md:26,61` |
| Macroscope 175 comments: 75 acted, 58 dismissed, 42 unknown, 43% | `docs/product/research/13-t3code-bot-comments.md:52` |
| Cursor Bugbot 135: 32 acted, 95 dismissed, 8 unknown, 24% | `docs/product/research/13-t3code-bot-comments.md:53` |
| CodeRabbit 60: 29 acted, 7 dismissed, 24 unknown, 48% | `docs/product/research/13-t3code-bot-comments.md:54` |
| Cursor Bugbot 70% dismissed | `docs/product/research/13-t3code-bot-comments.md:63` |
| Label definitions (changed, dismissed, unknown) | `docs/product/research/13-t3code-bot-comments.md:30-32` |
| The label is a proxy; a nearby fix can be missed | `docs/product/research/13-t3code-bot-comments.md:40-41`, `docs/product/TRACKER.md:62` |

Bar widths are the counts above divided by each bot's total (for example 95 / 135 = 70.37%).

## 3. How it works

| Claim | Source |
|---|---|
| Fast check: deterministic, cached, no model, no PR code | `docs/product/STRATEGY.md:66-67`, `docs/product/design/ACTION_MODE.md:371` |
| Fast check target under 2 minutes p95, not measured yet | `docs/product/design/ACTION_MODE.md:17,384-385` |
| Eight clauses | `internal/domain/fastcheck/fastcheck.go:13-20` |
| A finding needs a test that fails twice on the head and passes on the fixed tree; one red run and a build error are not proof | `docs/product/TRACKER.md:53`, `docs/product/STRATEGY.md:20-21` |
| A model may propose; rules decide; a missing fact is `unknown`, never a pass | `docs/product/STRATEGY.md:104`, `docs/product/design/ACTION_MODE.md:416`, `internal/domain/fastcheck/doc.go` (Invariants) |
| Approval counts on the head SHA from a non-author | `docs/product/design/ACTION_MODE.md:380` |
| Summary shows proven, unproven counted, and what was not checked | `docs/product/STRATEGY.md:70-71`, `internal/domain/summary/testdata/enforce_one_e1_bug.golden.md:12-41` |
| A person judges only what proof cannot settle | `docs/product/STRATEGY.md:37-38` |

## 4. Features

| Claim | Source |
|---|---|
| Status: merged into `product-build`; the Action wiring (AM15) is next; no customer runs it | `docs/product/TRACKER.md:60`, `docs/product/STRATEGY.md:115-120` |
| Fast check is a pure function; missing facts make a clause unknown | `internal/domain/fastcheck/doc.go` (Purpose, Invariants) |
| 383 of 383 t3code comments parse; Macroscope, Codex, Devin, Greptile, Copilot, CodeRabbit, Cursor Bugbot | `docs/product/TRACKER.md:63`, `docs/product/TRACKER.md:48` |
| Pinned tests read-only until owner approval; weakened tests and tautological asserts flagged; mock-of-subject in Go, Python, JS/TS; fail-twice rule | `docs/product/TRACKER.md:53` |
| Sandbox run 44910812111 on a GitHub-hosted runner: 34 fetch and 36 exec probe tests blocked, 0 unknown | `docs/product/design/ACTION_MODE_SPIKE.md:326,541`, `docs/product/TRACKER.md:49` |
| Tests run with no network; installs reach only an allowlisted registry through a proxy | `docs/product/design/ACTION_MODE_SPIKE.md:572-576` |
| Probe classes listed on the card | `docs/product/design/ACTION_MODE_SPIKE.md:550-578` |
| `/dev/shm` escape on an Azure host, run 44909950640, closed with a private tmpfs | `docs/product/design/ACTION_MODE_SPIKE.md:527,540` |
| Refuses to run when hardening does not take; proof is unknown | `docs/product/design/ACTION_MODE_SPIKE.md:519-526` |
| Sandbox not verified: load stress, fork bomb, non-Go package managers, self-hosted and ARM runners | `docs/product/TRACKER.md:49`, `docs/product/design/ACTION_MODE_SPIKE.md:583-595` |
| Log on `refs/heimdall/state`, HMAC per chain head, CAS via GraphQL `updateRefs` | `docs/product/design/ACTION_MODE.md:26`, `internal/adapters/gitlog/doc.go:20-22,33` |
| One winner in 14 of 14 trials | `internal/adapters/gitlog/doc.go:22` |
| A bad MAC fails closed | `docs/product/design/ACTION_MODE.md:196`, `docs/product/TRACKER.md:58` |
| Summary: 7 sections, 15 hostile strings escaped, 60,000-byte cap, weak finding marked proven shows unproven | `docs/product/TRACKER.md:52` |
| Shadow mode posts neutral "would block because" | `docs/product/design/ACTION_MODE.md:253,422` |
| Break-glass needs a reason and a named person, stays open until reviewed, never from an agent | `docs/product/design/ACTION_MODE.md:428-431`, `docs/product/TRACKER.md:45` |
| Test selection: Heimdall's suite 106.7 s to 13.1 s on 5 commits | `docs/sota/BENCH.md:145`, `docs/product/TRACKER.md:55` |
| Falls back to the full suite on go.mod, vendor or an unmapped file | `docs/product/TRACKER.md:55`, `docs/sota/BENCH.md:106-107` |
| Small gain on echo and client_golang | `docs/sota/BENCH.md:148-153` |
| Worst-case cost bound checked before each call | `docs/product/TRACKER.md:56,60` |
| Up to 3 attempts on 429 and 5xx with backoff, inside the cap | `docs/product/TRACKER.md:70` |
| Any OpenAI-compatible endpoint: Ollama, vLLM, LiteLLM, DeepSeek, a company gateway; no key header for a local endpoint | `internal/adapters/openrouter/doc.go:4-10` |
| Board sign-in: OIDC or GitHub with PKCE; roles viewer, approver, admin | `internal/transport/auth/doc.go:6-8` |
| Matched by a stable id, never by a login | `internal/transport/auth/doc.go:11-22`, `docs/product/TRACKER.md:47` |
| Live provider sign-in not verified | `docs/product/TRACKER.md:34,47` |
| Session readers for Claude Code, Codex, OpenCode, Cursor; read-only | `internal/adapters/sessions/doc.go:6-9` |
| Purpose: find repeated corrections | `docs/product/STRATEGY.md:74-76` |
| Real OpenCode and Cursor files not verified | `docs/product/TRACKER.md:35` |
| Rendered summary comment | `internal/domain/summary/testdata/enforce_one_e1_bug.golden.md:1-43` (rendered as HTML; Markdown escapes removed) |

## 5. Where we honestly are

| Claim | Source |
|---|---|
| Go: 7 valid bug, 3 valid clean (testify, gin, echo, chi, urfave/cli, logrus, cobra) | `docs/sota/BENCH.md:38-51,64` |
| t3code TypeScript: 10 valid bug, 2 valid clean | `docs/sota/BENCH.md:500` |
| t3code Rust: 1 valid bug | `docs/sota/BENCH.md:501` |
| Payment fixture: 4 valid (1 prompt-injection case scored clean, 3 clean); refunds case invalid | `docs/sota/BENCH.md:338-344` |
| Each verdict held on at least two runs | `docs/sota/BENCH.md:28-30,429,533` |
| Live run 1: recall 0 of 7, FP 0 of 2, $0.86 at high stated prices | `docs/sota/BENCH.md:288-289,293`, `docs/product/TRACKER.md:64` |
| Run 1: 0 findings proposed in all 10 cases; loop ended on the first answer without a tool call; one HTML 400 | `docs/sota/BENCH.md:299-306` |
| Run 1 fix: tool call required where allowed, text tool calls parsed, nudges before stopping | `docs/product/TRACKER.md:65`, `docs/sota/BENCH.md:311-314` |
| Live run 2: 18 cases, Go, TypeScript and Rust | `docs/sota/BENCH.md:589`, `docs/product/TRACKER.md:69` |
| Run 2: real tool call on every recorded turn; injection case proposed 2, unproven (`build_error_fixed`), no FP | `docs/sota/BENCH.md:632-635` |
| Run 2: 15 calls in 14 cases failed with HTTP 500; recall not judged | `docs/sota/BENCH.md:637-639`, `docs/product/TRACKER.md:69` |
| Run 2 cost $1.18 at high stated prices | `docs/sota/BENCH.md:626,643` |
| Run 2 fix: retry with backoff on 429 and 5xx (AM12c, merged); run 3 not run yet | `docs/product/TRACKER.md:70`, `docs/sota/BENCH.md:639-641` |
| Harness check with a scripted model: 2 of 2, not a quality score | `docs/sota/BENCH.md:186-189,215` |
| A test can prove a bug is real, never that a finding is false | `docs/product/STRATEGY.md:18` |

## 6. Compare

| Claim | Source |
|---|---|
| LLM reviewers comment; nobody drops unproven findings and prints the count | `docs/product/STRATEGY.md:99-100` |
| Rivals let a verifier model or a confidence score decide | `docs/product/STRATEGY.md:104-105` |
| Heimdall referees other bots' findings | `docs/product/STRATEGY.md:105`, `docs/product/design/ACTION_MODE.md:30` |
| Rivals' self-hosted review needs an enterprise contract | `docs/product/STRATEGY.md:108-109` |
| Heimdall runs on your own VM (server mode) | `docs/product/STRATEGY.md:117-120`, `README.md:73` |
| Bring your own key, any OpenAI-compatible endpoint | `docs/product/design/ACTION_MODE.md:15,28`, `internal/adapters/openrouter/doc.go:4-7` |

## 7. Install preview

| Claim | Source |
|---|---|
| The Action is not shipped (A7 open) | `docs/product/ROADMAP.md:28`, `docs/product/TRACKER.md:60` |
| Proof workflow trigger, permissions `contents: read`, `pull-requests: read` | `docs/product/design/ACTION_MODE.md:236` |
| 15-minute cap | `docs/product/design/ACTION_MODE.md:18` |
| `uses:` pinned by full SHA; binary SHA-256 checked | `docs/product/design/ACTION_MODE.md:241-242` |
| Model key on one step only | `docs/product/design/ACTION_MODE.md:244` |
| Two weeks in shadow mode, then required | `docs/product/STRATEGY.md:72` |
| Action name `heimdall/action` and its `with:` inputs | Placeholder. No source. Labelled as a placeholder on the page |

## 8. Close and footer

| Claim | Source |
|---|---|
| The repo is public | `gh repo view prathameshk421/Heimdall` reports `PUBLIC` (Nov 21 2026) |

## Left out (no source found)

- "Source-available" or a license name. STRATEGY 0.2 plans FSL, but the repo has no LICENSE file.
- A measured fast-check p95. Only the target exists.
- Any recall or false-positive rate from a real model. Neither live run produced one.
- "Typical AI reviewer stays silent on missing facts". Removed: no source.
