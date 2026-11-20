# 04. Eval on real repos: datasets, method, repos, PRs

Date: 2026-11-21. Author: research pass for Heimdall.
Status key: "checked" means I fetched the page or ran the command in this pass. "not verified" means I did not.
Facts about datasets come from web search summaries (secondary), unless the row says "checked".

## 1. Datasets and benchmarks

### 1.1 Code review benchmarks

| Name | Size | Languages | License | Measures | Fit for Heimdall |
|---|---|---|---|---|---|
| Greptile bug benchmark (2025) | 50 real PRs, 5 repos (sentry, cal.com, grafana, keycloak, discourse), one fixed bug each | Python, TS, Go, Java, Ruby | not verified | Catch rate: tool must comment on the buggy line and explain impact. Greptile reports 82%. | Good design to copy (before-bug and after-fix branches). Vendor-run and contested: Augment re-scored Greptile at 45% on the same repos. Small. Recall only. |
| Martian Code Review Bench, offline track (Feb 2026) | 50 PRs, 5 repos, 136 golden comments (Unblocked cites 173) | same 5 repos as Greptile | open source (exact license not verified) | Precision, recall, F1 against golden comments | Cheap sanity set. Shares ancestry with the Greptile set, so contamination risk for any model. |
| Martian Code Review Bench, online track | Live: reactions of developers to AI comments on real OSS PRs | many | not verified | F1 from whether devs act on comments | Shows what the field treats as a real metric. Cannot be replayed for our own agent. |
| GitHub ReviewBench (Oct 2025) | 219 real PRs, 187 repos | 19 languages | open (license not verified) | Grounded F1 on a gold set, plus augmented F1 that credits findings outside the gold set | Best breadth. The grounded vs augmented split is useful: out-of-gold findings are not always false positives. GitHub tests its own product on it. |
| Qodo Code Review Benchmark 1.0 | 100 merged PRs, 580 injected issues | 7 languages | not verified (HF dataset Qodo/PR-Review-Bench exists) | Precision and recall vs injected ground truth | Injected bugs are synthetic and mostly single-file (GitLab analysis). No clean-PR control found. Do not use as the main set. |
| LangChain ReviewBench (Jul 2026) | smaller, not verified | not verified | not verified | not verified | Name clash with GitHub ReviewBench. Skip unless needed. |
| SWR-Bench (arXiv 2509.01494, FSE 2026) | 1,000 verified PRs, 12 Python projects, full repo context | Python | not verified | LLM judge checks coverage of structured ground-truth issues | Repo-level context like Heimdall. Python only. Judge-based, so see section 4. |
| ContextCRBench (arXiv 2511.07017, FSE 2026 industry) | 67,910 entries | multi | not verified | Hunk quality, line-level defect localization, comment generation, with issue and PR text as context | Large training or tuning pool. Line-level defect localization maps to our finding location check. Not a proof-based metric. |
| CodeReviewer (Microsoft, FSE 2022) | large multilingual corpus of diffs and review comments | C, C++, C#, Go, Java, JS, PHP, Python, Ruby | not verified | Quality estimation, comment generation, refinement | Weak fit. Human comments are often style or noise, and the match metrics (BLEU) punish valid reviews. Use only to pull sample diffs. |
| CRScore (NAACL 2025) | metric plus about 2.6k to 2.9k human labels (sources disagree) | multi | not verified | Reference-free quality: conciseness, comprehensiveness, relevance. Spearman 0.54 with humans. | Not needed. Heimdall findings must be proven, so we score proof and truth, not prose quality. |

### 1.2 Issue resolving and test generation

| Name | Size | Languages | License | Measures | Fit for Heimdall |
|---|---|---|---|---|---|
| SWE-bench / Verified | 2,294 / 500 | Python (12 repos) | MIT (not verified) | Issue resolving with fail-to-pass (F2P) and pass-to-pass (P2P) tests | Source of F2P-labelled pairs. OpenAI stopped reporting Verified in Feb 2026 for contamination and flawed tests. Use for tooling checks, not headline numbers. |
| SWE-bench Multilingual | 300 tasks, 41 or 42 repos | 9 languages incl. Go, Rust, Java, TS/JS | not verified | Same, multi-language | Small, useful Go and Rust coverage. Repo list not checked. |
| Multi-SWE-bench (ByteDance) | 1,632 (arXiv) or 2,132 (NeurIPS listing), 39 repos | Java, TS, JS, Go, Rust, C, C++ | not verified | Issue resolving, with Docker per instance | Best ready-made Go source (cli/cli, grpc-go, go-zero). Gives F2P tests we can use as ground truth for the test agent. |
| SWE-bench Pro (Scale) | about 1,865 tasks, 41 repos (731 public) | Python, JS, TS, Go | public set reported as copyleft repos, not verified | Harder, longer tasks | Less contaminated than Verified. Public set only for us. |
| SWE-rebench V2 / V2-PRs (Nebius) | 126,300 PR samples (V2-PRs); V1 is 21k Python | Go, Python, JS, TS, Rust, Java and more | not verified | Executable tasks at scale, leaderboard snapshots | Biggest pool for Go. A July 2026 hard slice has 111 tasks, 21 in Go. Good for fresh, post-cutoff samples. |
| SWE-Gym | not found in this pass | Python | not verified | Training environments | not verified. Skip. |
| SWT-bench (ETH, arXiv 2406.12952) | 1,983 instances; Lite 276 | Python | not verified | Reproduction test from an issue: must fail before the fix, pass after (F2P), plus coverage of changed lines | Closest to our red-first test agent. Use its F2P rule as the pass criterion. Python only. |
| Multi-SWT-Bench (arXiv 2609.34752) | 1,963 instances, 51 repos | 8 languages | not verified | Same as SWT-bench, multi-language | Same, with Go. Not opened in this pass: title and size from search only. |
| TDD-Bench Verified (IBM) | 449 issues | Python | not verified (repo IBM/TDD-Bench-Verified) | F2P plus coverage of the change, derived from SWE-bench Verified. Best model 23.6% F2P in the paper. | Cleaner subset than SWT-bench. Use as a calibration check on the test agent. |
| TestGenEval (Meta, ICLR 2025) | 68,647 tests, 1,210 file pairs, 11 repos; Lite 160 | Python | not verified | Coverage and mutation score of generated unit tests | Not red-first. Useful only for the mutation-score idea. |

### 1.3 Bug and vulnerability datasets

| Name | Size | Languages | License | Measures | Fit |
|---|---|---|---|---|---|
| Defects4J | 835 bugs v2.0 (sources say 835 to 854), 17 projects | Java | MIT (not verified) | Reproducible bugs with a bug-revealing test | Old, widely in training data (leak shown with StarCoder). Use for tooling only. |
| BugsInPy | 493 bugs, 17 projects | Python | not verified | Same | Same. Reproducibility not tracked. |
| BugsJS | 453 (some tables say 513), 10 projects | JS (Node) | not verified | Same | Same. |
| GHRB / GitBug-Java | 76 to 89 bugs; GitBug-Java 199 bugs, 55 repos | Java | not verified (GitBug-Java paper CC BY-SA 4.0) | Recent bugs after training cutoffs | Shows the right idea: pick bugs newer than the model cutoff. Java only. |
| GoBench (CGO 2021) | 82 real bugs (GoReal), 103 kernels (GoKer), 9 apps | Go | not verified | Go concurrency bugs | Only Go-specific set found. Real bugs need old Docker, kernels are small. Good for a deterministic-tool track (race detector). ConcurBugBench (about 200 bugs, Go and others) is in development. |
| Vul4J / Vul4J+ | 79 / 191 vulns, with proof-of-vulnerability tests | Java | not verified | Vulns with a witnessing test | Good match for "proven by a failing test". Java only. |
| CVEfixes, CrossVul, BigVul, PrimeVul | about 168k, multi-lang, about 188k, about 236k functions | C/C++ mostly; CVEfixes and CrossVul multi-lang | not verified | Vulnerable vs fixed functions | Label noise is high in BigVul and CrossVul (reported 25 to 48% wrong or noisy). PrimeVul was cleaned but is C/C++. Not a fit for Go-first. |
| SecurityEval | not checked | Python | not verified | Security of generated code | Not a review benchmark. Skip. |
| OSV / GHSA fix commits | thousands, Go ecosystem included | all | CC0 for OSV data (not verified) | Fix commit per advisory | Best source of Go security fix commits. Needs our own harness (section 2). |

### 1.4 SZZ tools

| Tool | What it is | Notes |
|---|---|---|
| PySZZ (grosa1/pyszz, v2 at grosa1/pyszz_v2) | Python, several variants (B, AG, L, R, RA, MA) | Input is a JSON list of bug-fixing commits. Paper: Rosa et al., ICSE 2021 oracle study. Repo link seen only via a mirror and slides: not verified on github.com. |
| SZZ Unleashed (mrksbrg/SZZUnleashed) | Java and JGit, MIT, line-number mapping | Works on any language in git. Does not skip refactors or meta-changes. |
| OpenSZZ (clowee/OpenSZZ) | Cloud web service, B-SZZ | Simplest, noisiest. |
| PyDriller | General git mining, has B-SZZ | Handy for the harvesting step. |

Warning: SZZ output is noisy. Treat each candidate as a hypothesis and confirm by hand or by test (section 2).

## 2. Method to build our own eval

### 2.1 Pipeline (three tracks)

Track A: reviewer recall on bug-introducing PRs.
1. Pick a repo. List fix commits: PRs or issues with words like "regression", "fixes #", "introduced in #", or Revert PRs. Also use OSV advisories for security fixes.
2. Find the bug-introducing change. Use the fix PR text first (many name the PR, see section 5). Run PySZZ on the rest.
3. Gate: keep the pair only if (a) the fix touches the lines the introducing PR added, and (b) a test fails on the introducing merge commit and passes on the fix (run in the container). Drop the rest. This is the same F2P rule as SWT-bench.
4. Replay: recreate the introducing PR against its parent in a throw-away fork (head = merge or PR head, base = parent). Run the reviewers. The fix and later history must not be visible.
5. Score by the label: the reviewer "found" the bug if it filed a finding on the buggy hunk and that finding's own proof (failing test or tool result) matches the F2P behaviour, or a human-written one-line root cause matches it. Do not use the fix diff as the answer shown to a judge model alone.

Track B: false positives on clean PRs.
1. Take merged PRs that are old enough (at least 12 months) that no later fix, revert or regression issue cites them. Check with `gh search` for "#N" in later PR bodies and with SZZ (no fix commit blames its lines).
2. Mix PR types: refactors, dependency bumps, docs, small features. Refactors and modernizer PRs are the best stress test for false positives.
3. Every finding Heimdall files on a clean PR is reviewed by a human into three labels: real defect (a true positive the label missed), wrong, or nit. Report rate per PR and per 1,000 changed lines.
4. Because Heimdall needs proof, also count "proof failed to run" separately from "wrong proof".

Track C: red-first test generation.
1. Take issue plus fix PR pairs where the PR changes both source and tests (SWE-bench style).
2. Give the test agent only the issue text and the repo at the parent commit.
3. Pass = generated test fails on parent for the issue's reason, and passes on the fix commit (F2P). Also check coverage of the fixed lines (TDD-Bench) and that the failure message is not a compile error or a missing import (Heimdall already rejects compile errors).
4. Extra check: run the test on the parent twice and on the fix twice to catch flaky tests.

### 2.2 Metrics

- Reviewer recall (Track A): found / total, with Wilson 95% interval, split by bug class (logic, concurrency, API contract, security).
- Reviewer precision and false-positive rate: precision on Track A (findings on the buggy PR that are real) and findings per clean PR on Track B.
- Proven-finding rate: share of findings that carry a valid proof. Heimdall should report this next to recall.
- Time to first finding and cost per PR.
- Test agent: F2P rate, coverage of changed lines, flaky rate, compile-error rate.
- Always report a baseline: a plain single-prompt LLM review with the same model, and a deterministic tool baseline (go vet, staticcheck, golangci-lint, the race detector).

### 2.3 Sample sizes

Wilson 95% half-width at a true rate of 50% is about 9.8 points for n = 100 and about 6.9 for n = 200. For a false-positive rate near 5%, n = 100 clean PRs gives about 4 points. So:
- Track A: 100 PRs (60 Go, 40 other) for a first number; 200 to compare two configurations.
- Track B: 100 clean PRs, same repo mix.
- Track C: 60 issues (30 Go, 30 Python or TS) for a first number.
- Run each config 3 times (LLM variance) and report the mean and spread.
- Keep 20% of pairs as a frozen holdout. Never tune prompts on it.

### 2.4 Cost estimate (not verified: prices and token use are assumptions)

Assumptions: DeepSeek-class input at about $0.3 per 1M tokens. Output tokens at roughly $1 per 1M (assumption, check before running). Cache hits would lower cost. One reviewer run on one PR: 150k input and 15k output tokens (several agent turns plus tool output). One test-agent run: 300k input and 30k output.

| Item | Runs | Input tokens | Output tokens | Cost |
|---|---|---|---|---|
| Track A, 100 PRs x 3 repeats | 300 | 45M | 4.5M | 13.5 + 4.5 = about $18 |
| Track B, 100 PRs x 3 repeats | 300 | 45M | 4.5M | about $18 |
| Track C, 60 issues x 3 repeats | 180 | 54M | 5.4M | 16.2 + 5.4 = about $22 |
| Human or judge labelling (LLM judge, 2 per finding, small) | about 2k calls | 10M | 1M | about $4 |
| Total first full run | | about 154M | about 15M | about $62 |

Smoke run (10 PRs per track, 1 repeat): about $2 to $3. This is near the $3 testing cap in CLAUDE.md. A full run needs an owner yes (R13 and the cost rule). Plan for 2 to 3 full runs: about $120 to $190. Container CPU time is extra and small.

## 3. Repo shortlist

Runtime column: "measured" = I ran `go test ./...` on a shallow clone on this Mac (Go 1.27.3, CGO off, cold module cache, network on, so it includes module download and build). A real offline container needs a pre-filled module cache or `go mod vendor`. All other runtimes are not verified.

| # | Repo | Lang | Test command | Runtime | Why |
|---|---|---|---|---|---|
| 1 | spf13/cobra | Go | `go test ./...` | 6 s measured, pass | Small, many regressions with named PRs |
| 2 | gin-gonic/gin | Go | `go test ./...` | 23 s measured, pass | Large PR flow, recent regression with a clean intro PR |
| 3 | go-chi/chi | Go | `go test ./...` | 27 s measured, pass | Small, middleware logic |
| 4 | labstack/echo | Go | `go test ./...` | 9 s measured, pass | Known perf regression PR |
| 5 | uber-go/zap | Go | `go test ./...` | 5 s measured, pass | Strong tests, no regression PR found in this pass |
| 6 | stretchr/testify | Go | `go test ./...` | 17 s measured, pass | Two verified regression pairs, mock logic |
| 7 | prometheus/client_golang | Go | `go test ./...` | 21 s measured, pass | Concurrency regression with later test |
| 8 | caddyserver/caddy | Go | `go test ./...` | 189 s measured, 3 FAIL packages in my sandbox (likely network or env; not investigated) | Big, realistic, many fixes. Needs a tuned test subset. |
| 9 | etcd-io/etcd | Go | `go test` on selected modules (e.g. `./server/...` units) | not verified (full suite is long) | Concurrency regression with named PR |
| 10 | hashicorp/go-retryablehttp | Go | `go test ./...` | not verified | Small HashiCorp lib. No bug pair found. Search returned nothing. |
| 11 | hashicorp/go-multierror | Go | `go test ./...` | not verified | Small. No bug pair found. |
| 12 | psf/requests | Python | `pytest -x tests` (uses local httpbin) | not verified | Verified regression pair (2024) |
| 13 | encode/httpx | Python | `pytest` | not verified | Clean test suite |
| 14 | pallets/flask | Python | `pytest` | not verified | Small, clean tests |
| 15 | pydantic/pydantic | Python | `make test` or `pytest` | not verified (Rust core build adds minutes) | Fix PR found, intro PR not checked |
| 16 | expressjs/express | TS/JS | `npm test` (mocha) | not verified | Clean-PR source |
| 17 | colinhacks/zod | TypeScript | `pnpm test` (vitest) | not verified | Verified revert-style pair (2026) |
| 18 | tj/commander.js | JavaScript/TS typings | `npm test` (jest) | not verified | Verified small revert pair (2023) |
| 19 | date-fns/date-fns | TypeScript | `npm test` | not verified | Many tiny PRs, clean-PR source |
| 20 | FasterXML/jackson-databind | Java | `mvn -q test` | not verified (long; use one module test class subset) | Verified-intro regression (2025) |
| 21 | google/gson | Java | `mvn -q test` | not verified | Small, fast |
| 22 | BurntSushi/ripgrep | Rust | `cargo test --workspace` | not verified | Old but named regression pair (2018) |
| 23 | clap-rs/clap | Rust | `cargo test --workspace` | not verified | Many fixes. No intro pair checked. |
| 24 | serde-rs/serde | Rust | `cargo test` | not verified | Search found no regression PR. Clean-PR source only. |

Note: the date range matters more than the repo. For Track A use PRs merged after the model's training cutoff when you can (section 4). Several pairs below are from 2026 for that reason, but I did not check the cutoff of the model we use.

## 4. How competitors publish numbers, and the critiques

- Vendors run their own benchmark and win. Greptile reports 82% on its set; Augment re-ran it and reports Greptile at 45% and itself at 59% F1. Qodo, CodeRabbit, Baz, Cubic and GitHub each publish a first place on their own or a favourable set (DeepSource post "Every AI code review vendor benchmarks itself, and wins", seen in search only).
- Shared ancestry. Qodo argues that the Greptile, Augment and Martian offline sets come from the same 50 PRs, so scores are not independent.
- Martian moved its headline to an online metric (developer reactions) because offline results did not match field use. Online metrics measure what people accept, not what is correct.
- Test dates differ. GitHub ReviewBench results were taken on different dates per tool (Copilot Oct 1, others June), and products change between runs.
- Injected bugs are synthetic and mostly single-file (Qodo, per a GitLab analysis). Real bugs are harder and span files.
- No clean-PR control. In the Qodo benchmark I found no negative controls, so precision is only measured against injected issues. Greptile's design has one bug per PR and counts a catch, not a false alarm.
- Judge bias. Matching findings to ground truth with an LLM brings self-preference and preference leakage (ICLR 2026 paper on preference leakage; an April 2026 paper on self-preference bias). Our rule: the judge model is not the model under test and not from the same family; use two judges; spot-check 10% by hand. Better still, use executable proof (F2P tests) where we can.
- Contamination. OpenAI stopped reporting SWE-bench Verified in Feb 2026 (contamination, flawed tests). The "SWE-Bench Illusion" paper shows models can name file paths without repo context. Defects4J leaks into StarCoder data. Fixes: use post-cutoff PRs (GHRB, SWE-rebench style), private or newly mined sets, and report results on old and new PRs separately.
- Metric choice. BLEU-style comment match punishes valid reviews (CRScore paper). F1 on matched findings hides the false-positive cost for a human reader: also report findings per PR.
- Label noise. SZZ is noisy and vulnerability datasets have 25 to 48% bad labels (reported). We add a gate (the F2P test) to cut this.

Our publishing rules: give n, intervals, repeat count, the exact commit hashes, dates, model ids and cost; publish the pair list and the harness; mark anything not run as "not verified".

## 5. Specific PR and commit pairs (Track A) with URLs

"Intro" is the change that created the bug. "Fix" is the PR that repaired it. Each row says what I checked. All were checked with the GitHub API on 2026-11-21 unless noted.

| # | Repo | Intro | Fix | What I checked |
|---|---|---|---|---|
| 1 | stretchr/testify (Go) | https://github.com/stretchr/testify/pull/1182 (merged 2022-06-14, touches mock/mock.go; title is "Added Go 1.18.1 as a build/supported version") | https://github.com/stretchr/testify/pull/1207 (nil mutex panic in AssertExpectations) | Fix body names #1182. Intro touches mock/mock.go: checked. The title does not match the mutex change: confirm the diff before use. |
| 2 | stretchr/testify (Go) | https://github.com/stretchr/testify/pull/1212 (merged 2022-06-24) | https://github.com/stretchr/testify/pull/1795 (panic in AssertExpectationsForObjects, merged 2025-09-15) | Fix body says #1212 broke by-value mocks. Merge dates checked. Bug lived 3 years. |
| 3 | spf13/cobra (Go) | https://github.com/spf13/cobra/pull/889 (bash shellcheck fixes, merged 2019-07-11) | https://github.com/spf13/cobra/pull/1001 (global `$c` no longer set in custom_func) | Fix body names #889. Both merged: checked. Bash completion script, so the test is a script test. |
| 4 | gin-gonic/gin (Go) | https://github.com/gin-gonic/gin/pull/4181 (refactor SaveUploadedFile, merged 2025-03-18) | https://github.com/gin-gonic/gin/pull/4702 (skip chmod on existing dirs, fixes issue 4622) | Intro diff contains `Chmod`: checked. Link to #4622 is from the fix body. I inferred #4181 as the intro from the diff, not from a stated link. |
| 5 | etcd-io/etcd (Go) | https://github.com/etcd-io/etcd/pull/19139 (avoid deadlock in Close, merged 2025-01-09) | https://github.com/etcd-io/etcd/pull/19221 (race condition, "also a regression of PR 19139") | Title of fix names #19139: checked. Concurrency bug, good for race detector track. |
| 6 | prometheus/client_golang (Go) | commit 1173d734050af725504a9b2fbe93bff0b16f1b53 (removed the push function, per the fix body) | https://github.com/prometheus/client_golang/pull/576 (fixes issue 574, panic) | Fix body names the commit. The commit's own title ("Increase minimum required Go version to 1.9") is not obviously the removal: not verified. |
| 7 | prometheus/client_golang (Go) | the package-level `emptyLabels` map in promhttp (issue https://github.com/prometheus/client_golang/issues/1274) | https://github.com/prometheus/client_golang/pull/1318 (merged 2023-08-01); test added later in https://github.com/prometheus/client_golang/pull/2000 | Issue and PRs checked. Intro PR not identified: run SZZ on the #1318 diff. PR 2000 (2026) gives a ready failing test. |
| 8 | labstack/echo (Go) | commit 2d79ff3d6c27592d01a820d93d0fd7887a87a4b4 (named in issue https://github.com/labstack/echo/issues/1777) | https://github.com/labstack/echo/pull/1799 (merged 2021-03-08, fixes perf regression) | Issue text and fix checked. The commit is a merge of #1628 by title. A performance bug, so the proof is a benchmark: weaker for "failing test". |
| 9 | psf/requests (Python) | https://github.com/psf/requests/pull/6655 (TLS settings in pool selection, merged 2024-03-11) | https://github.com/psf/requests/pull/6716 (re-enable custom SSLContext in adapters, merged 2024-05-24, closes issue 6715) | Fix body says #6655 broke it: checked. Shipped in v2.32.0 (2024-05-20). |
| 10 | psf/requests (Python) | https://github.com/psf/requests/pull/6667 (avoid reloading root certs, merged 2024-05-15) | https://github.com/psf/requests/pull/6724 (do not create default SSLContext if ssl missing, merged 2024-05-29) | Fix body says "recent regression"; its link to #6667 is my inference from dates and topic, not stated. Not verified. |
| 11 | FasterXML/jackson-databind (Java) | commit d7adffa7bd9f1882ec3db3c431f74d92502049ab ("Fix #4997: add null checks for ObjectNode keys (#5012)", 2025-03-07) | https://github.com/FasterXML/jackson-databind/pull/5099 (regression in ObjectNode.with, merged 2025-04-22) | Fix body names the commit; commit message checked. |
| 12 | tj/commander.js (JS/TS typings) | https://github.com/tj/commander.js/pull/2013 (factory functions re-export, merged 2023-09-12) | https://github.com/tj/commander.js/pull/2014 (revert, TS circular alias error) | Both checked. Caught by a CI type check: a deterministic-tool case. |
| 13 | colinhacks/zod (TypeScript) | https://github.com/colinhacks/zod/pull/6432 (merged 2026-08-19) | https://github.com/colinhacks/zod/pull/6438 (revert of an "OPTION B PROTOTYPE" hunk that changed accept rules) | Fix body names #6432: checked. Post-cutoff, so low contamination. |
| 14 | colinhacks/zod (TypeScript) | the Error-inheritance change that #4424 reverted (intro PR not identified) | https://github.com/colinhacks/zod/pull/4424 (merged 2025-05-19) | Revert PR checked. Intro PR not found in this pass. Break was in snapshot tests, a behaviour change. |
| 15 | BurntSushi/ripgrep (Rust) | the fix for issue #705 (a commit in the `ignore` crate, not identified) | https://github.com/BurntSushi/ripgrep/pull/827 (extra stat per entry on Windows, merged 2018-02-21) | Fix body checked. Intro commit not located. Windows perf bug: poor fit for a container. Use only if the intro is found. |

Candidate clean PRs for Track B (merged, small; "clean" is not verified. I did not search for later fixes or reverts):
- https://github.com/go-chi/chi/pull/1051 (stringscutprefix modernizer)
- https://github.com/labstack/echo/pull/2969 (replace Split in loops with SplitSeq)
- https://github.com/labstack/echo/pull/2822 (replace interface{} with any in context.go)
- https://github.com/prometheus/client_golang/pull/2021 (replace interface{} with any)
- https://github.com/colinhacks/zod/pull/5689 (sideEffects false in stub package.json)
- https://github.com/spf13/cobra/pull/1643 (deprecate ExactValidArgs, add arg-validator tests)

Modernizer and refactor PRs are good traps: a reviewer that files a finding there is likely wrong. Pick the real set with the Track B rule in section 2.

Not found in this pass: bug-intro pairs for uber-go/zap, go-chi/chi, hashicorp libs, flask, httpx, express, date-fns, gson, clap, serde. Mine them with SZZ.

## 6. Next steps (small)

1. Write a harvester: `gh search` for fix PRs with "regression" or "introduced in #", write a JSON list of (repo, intro, fix, test command).
2. Run PySZZ on fix commits that name no intro PR. Review output by hand.
3. Build a per-repo container with a warm module or package cache so tests run offline. Time each one, record in the table.
4. Run the smoke set (10 PRs per track) and report cost before the full run.

## Sources checked

Checked directly (gh api, this pass): all PR, issue and commit facts in section 5, and Go test timings in section 3.
Seen only in web search summaries: sizes and claims in sections 1 and 4, including arXiv ids 2509.01494, 2511.07017, 2406.12952, 2412.02883, 2410.00752, 2504.02605, 2409.19801, 2502.01534, and pages at greptile.com/benchmarks, github.blog ReviewBench, qodo.ai benchmark, openai.com "why we no longer evaluate SWE-bench Verified", deepsource.com blog. I did not open these pages with a fetch tool.
