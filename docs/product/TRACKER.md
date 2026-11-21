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
| Critic 3 on draft 2 | OpenCode / DeepSeek free | `CRITIC-v2.md` | done | three products glued; MergeHarbor; asymmetry of proof |
| A1 Action mode design | Claude Personal / Opus | `a1-action-design` | running | design doc only |
| A9 board login and roles | Claude Personal / Opus | `a9-board-auth` | merged (`20e8473`) | delegate stopped at the usage limit before its checks; coordinator reviewed (sealed `__Host-` cookies with name as AD, constant-time state, PKCE, ID token sig/iss/aud/exp/nonce, verified email only, safeNext, dev mode loopback only, no code or token in logs), ran checks and committed. gofmt clean, vet 0, lint 0 issues, archcheck holds, unit 3504 on branch and 3756 after merge. make verify, the secret scan and a live provider sign-in not verified. Follow-ups below |
| B1 OpenCode and Cursor session readers | Claude Personal / Sonnet | `b1-session-readers` | merged into `product-build` | make fast exit 0, 3557 tests after merge; e2e cards scan on a synthetic HOME with a real worktree; real OpenCode/Cursor files not read (schema drift not verified) |
| 11 MergeHarbor and rivals | Claude Personal / Sonnet | `research/11-mergeharbor-and-rivals.md` | done | MergeHarbor claims true in its docs; no users found; Heal fail-twice rule |
| A1 Action mode design | Claude Personal / Opus | `a1-action-design` | merged `c1f662b` | design only; GitHub facts not verified until spike S1; Q1 to Q10 take the doc's recommendations until the owner says otherwise |
| AM1, AM2, AM4 verdict, action policy, fast check | Claude Personal / Sonnet | `am-fastcheck` | merged `3dd6c24` | reviewed by coordinator: gate change additive, `Evaluate` never sets `Unknown`, merge rule unchanged; lint 0 issues, archcheck holds (102 packages), unit 3692 tests; make verify not verified |
| AM8 git-ref event store | Claude Personal / Opus | `am8-gitlog` | merged (code `0dacc6e`) | create by REST POST, move only by GraphQL `updateRefs` with `beforeOid` (the store's `Git` interface has no PATCH), tip reread after any refusal: tip = after is success, tip = before retries 1/2/4 s through an injected wait, other tip is a conflict; FORBIDDEN fails closed. New ports `GitReader`, `GitRefSwapper` (`internal/ports/vcs_gitstate.go`). Breaks caught: swapped tip comparison, and REST PATCH (15 tests fail). Coordinator reviewed `moveRef` edge cases. Lint 0 issues, archcheck holds (109 packages), swap and race tests pass `-count=20`, unit 4305 after merge. The branch's `docs/PROGRESS.md` commit `7f44590` is left out of product-build (Stage 0 owns that file). Live GitHub, App token path, GHES GraphQL, `-race` (needs cgo) not verified |
| AM6 coverage parsers | OpenCode / DeepSeek free; rework on Claude Personal / Sonnet | `am6-coverage` | merged | rework `ab75685`: Cobertura in `internal/adapters/coverage` on `encoding/xml` (Strict, depth 32, size checked first, one external DOCTYPE allowed because coverage.py and Java Cobertura write it, internal subsets refused); domain `Builder`; `Missing` and `Measurable()` fail closed. Fuzz 30 s each, 3M+ execs, pass. Lint 0 issues, archcheck holds (107 packages), unit 4060 tests after merge. Real reports beyond one golden and the gate wiring (AM14) not verified |
| AM10 model chat with tools | OpenCode / DeepSeek free | `am10-model-chat` | merged | reviewed by coordinator (cost math, bounded body, no key in errors); lint 0 issues, unit 3579 tests; no live model call |
| S1 GitHub spike | Claude Personal / Sonnet | `s1-github-spike` | merged `1d31cfd` | `52ed487`, `ACTION_MODE_SPIKE.md`: REST `force:false` is no CAS on `refs/heimdall/*` (stale, rewind, unrelated all 200); GraphQL `updateRefs` with `beforeOid` is a real CAS with `GITHUB_TOKEN`; Q3 closed: the ref (clones skip it, writes start no workflows); comment API took 262,144 bytes (docs say 65,536); blob 10 MiB ok; push to check p50 6.1 s, p95 8.8 s (n=10); cancel cut at 300 s. `workflow_run` facts blocked: need a default-branch workflow, an environment and a fork from `rekdfr`, each a separate owner yes. Cleanup verified; closed PR #17 and two check runs cannot be deleted by API |
| AM5 proof outcome and trivial skip | Claude Personal / Sonnet | `am5-proof` | merged | delegate `e26824d`; coordinator review moved the duplicated Heimdall-file rule into one `policy.Action.Protects` used by both the fast check and the trivial skip (two copies could drift into a bypass), with a table test. Lint 0 issues, archcheck holds (105 packages), unit 3906 tests after merge. Real GitHub file lists (binary, rename) and AM14 integration not verified |
| AM12 agent loop | Claude Personal / Sonnet | `am12-agentloop` | merged | delegate `a9b75ad`; coordinator review added the section 7 per-run input and output token caps (stop as `StopBudget`) and made the per-answer output cap required. Lint 0 issues, archcheck holds (104 packages), unit 3776 tests after merge. No live model run, no real sandbox tools (AM11): not verified. AM5 must map `StopNoSubmission` and `StopModelError` |
| AM3 Action events | Claude / Sonnet | `am3-action-events` | merged | `8c4ba65`: 9 events after the server's 52 (no position moves), optional `View.Action` and `FindingInfo.Proof`, money in micro-dollars, `fast.reported` refused when verdict and clauses disagree, break-glass under invariant 1 (never from an agent), state.reset keeps observed facts; existing view goldens unchanged. Lint 0, archcheck holds, unit 4704. TECH_SPEC 3.2 not updated |
| AM6r coverage on real OSS reports | Claude Personal / Sonnet | `am6-real` | merged | `02b4d1a`, `research/12-coverage-real-reports.md`: uuid v1.6.0, cobra v1.10.2 (Go set and atomic), commander.js v15.1.0 (c8 lcov), ms (Jest lcov), attrs 26.1.0 and more-itertools v11.2.1 (coverage.py Cobertura and lcov). LCOV and Cobertura totals equal each tool's summary exactly; Go checked against `go tool cover -func` and an independent count; every diff computed twice (parser and an independent Python reader) and agrees; a wrong Prefix makes Measurable false. Defect D1 fixed test first: a Go block ending at column 1 counted the closing-brace line (100 of 148 multi-line blocks in uuid). Lint 0 issues, archcheck holds, unit 4377 after merge. Java report and absolute-path LCOV from a real tool not verified |
| A9b sign-in by stable id | Claude Personal / Sonnet | `a9b-stable-id` | merged | `2d96eb1`: GitHub by numeric `id` (int64, zero, negative, text, float, huge refused), OIDC `sub` by default, no login fallback, config refuses a GitHub person without `github_id`, session identity re-checked per request in constant time, old cookies fail to open. Break attempt: guard mutated, test caught it. Lint 0 issues, archcheck holds, unit 4129 after merge. Live provider sign-in not verified. Operators: GitHub configs need `github_id`; email OIDC needs `login_claim = "email"`; everyone signs in once after deploy |
| AM7 third-party comment parsers | Claude Personal / Sonnet | `am7-external` | merged | `3b68d20`: CodeRabbit, Copilot, Cursor Bugbot, Greptile by Bot type + login in `sources` + body mark; mapping through diff hunks reviewed by coordinator; typed drops; bounds in doc.go. Fuzz pass, 64 KiB pathological bodies under 200 ms. Lint 0 issues, archcheck holds (108 packages), unit 4210 after merge. Goldens hand-made from documented formats: real bot comments not verified (A5) |
| AM11 isolated sandbox + escape probes | Claude / Opus | `am11-harden` | merged | run 44910812111 on GitHub-hosted ubuntu-latest: 34 fetch and 36 exec probe tests blocked, 0 unknown. Closed: path and abstract Unix sockets (0 of 77 exec, 0 of 79 fetch), DNS and resolved, D-Bus, docker, journald, network (exec only `lo`; fetch only the proxy, 8 disallowed CONNECTs got 403), parent /proc, ptrace, other users' files, setuid, keyrings, cgroup, eBPF, namespaces, survivors, timeout. Found and closed a second escape: shared host `/dev/shm` on an Azure host (run 44909950640), now a private tmpfs per service. `Setup` self-checks each phase and refuses (`CannotIsolateError`, proof unknown) when the hardening did not take. Coordinator review: replaced a `//nolint:gosec` from 78816cc with the repo's `#nosec G204` pattern (R2), lint 0 on darwin and linux. Not verified: resource-limit stress, fork bomb, non-Go package managers, self-hosted and ARM runners; PR code can still send data to an allowed registry (no secret is readable). Left for the owner: fixture branch `heimdall-test/am11-r3` and its 5 runs |
| AM9 GitHub adapter additions | Claude Personal / Sonnet | `am9-github` | merged | `40b792d`: static token, comments by marker and author, artifact zip (streaming cap, entry count, per-entry limit read through, path escape, symlink and duplicate refusal), code scanning, Git Data (no CAS claimed). New ports in `internal/ports/vcs_action.go`. Coordinator kept 422 untyped: the shared adapter's test asserts it on purpose and the live gate uses it; AM8 calls GetRef instead. Lint 0 issues, unit 4109 after merge. Live GitHub calls, cross-host artifact redirect, comment size limit not verified |

| A3 proof summary renderer | Claude / Sonnet | `a3-summary` | merged | `3a1fd9e`, new `internal/domain/summary`: 7 sections, hidden marker, shadow wording, "unproven, not shown", 15 hostile strings escaped, 60,000-byte cap, fail closed (E4 marked proven shows unproven, ULID shows as F?). 7 goldens. GitHub rendering of escapes not verified; break-glass line waits for A8 |
| A6 test integrity rules and the fail-twice proof rule | Claude / Sonnet | `a6-test-integrity` | merged | `9582565`: pinned tests read-only (owner approval clears), weakened tests and tautological asserts warn, mock-of-subject (Go, Python, JS/TS) warns, `Prove`: proven needs 2 red runs on head and green on the fixed tree; flaky, one red run, build error never proof (reviewed by coordinator). Lint 0, archcheck holds. Real diffs not parsed yet; caller must pass only `Result.Blocking()` to fastcheck |

| A13 test selection | Claude / Sonnet | `a13-test-selection` | merged | `6ddf435`: reverse-dependency closure, fail safe to Full (go.mod, go.sum, go.work, vendor, unknown graph, unmapped file). Apple M3, warm cache, 5 real commits each: heimdall 106.7 s to 13.1 s; echo 8.5 to 8.4 s; client_golang 51.7 to 43.2 s; selection costs 0.2 to 0.6 s. No large repo, no recall test (all commits green), Go only, nightly full run not built |
| P2.1b Proof Bench reviewer harness | Claude / Opus, then OpenCode / DeepSeek deploy | `p2.1b-bench-review` | merged, live run pending | `8953e75`: fake-model check on real cases: testify-1207 proven hit (1.9 s), chi-1185 proven mapped hit (105.8 s, its suite is 25 s per run), cobra-1643-clean 0 false positives. Cost cap checked before each call with the worst-case bound (coordinator reviewed); key read through config only. Live run: owner runs it (cap $3, estimate $0.15 to $0.50). Real-model recall, FP and evidence rate not verified |
| AM13 Action rows | Claude / Sonnet | `am13-action-rows` | merged | `8054225`: `actionflow.Decide` gives 9 rows (state, fast, ingest, proof, findings, budget, break_glass, sweep, publish) with typed effects; results count only for their head, base and policy hash; fail closed on bad policy or failed log. Tested on hand-built views, not on Fold of real events. AM14 must pass HeadAt (push time) |
| AM14 Action gate, proof and ingest use cases | OpenCode / DeepSeek V4.1 Flash (deploy) | `am14-action-app` | AM14a merged (branch `am14-action-app-gate`); AM14b not started | AM14a `3571210`, `c53b8b6`, `a5d2384`, `eb52cd0`, and the effect-order fix: new `internal/app/action` (`doc.go`, `ingest.go`, `gate.go`, `gate_effects.go`, `gate_view.go`, `gate_test.go`). Ingest maps third-party comments with `external` and merges a candidate into our finding on same file, overlapping lines and claim Jaccard at least 0.6. The gate loads through `ports.EventStore`, appends `state.reset` first on a failed MAC or chain and fails closed with `ErrStateUnverified` when no `Resetter` is wired, folds, reads GitHub, runs `fastcheck.Evaluate` with only `testintegrity.Result.Blocking()`, calls `actionflow.Decide` with `HeadAt` set to the push time, and runs the rows in rounds. `PostChecks` goes before the summary so a crash repeats an idempotent post. Only an appended event counts as progress, so a second run on the same head appends nothing, posts one check run per head and leaves one comment. Component test: in-memory fakes for every port, nine cases (enforce pass, shadow neutral, forged pass gives unknown, warning-only integrity does not block, no proof check without a result, bad MAC resets and gives unknown, bad MAC without a resetter fails closed, double run posts one comment, moved head re-decides). `CGO_ENABLED=0 go test ./internal/app/action/...` pass; `golangci-lint` 0 issues; archcheck holds (115 packages); `make fast` 4819 tests. Not verified: no real GitHub call, no `gitlog` against `httptest`, no live `pr.opened`/`pr.pushed` fold, and no `FactsReader` implementation yet (cmd wiring is AM15). Proof effects are ignored by design. Coordinator review: a failed comment read was swallowed (R6); fixed in `cb10692` with `Result.IngestFailed` and a test; a dead loop removed. The "forged artifact" test only covers a missing fact; real artifact parsing and validation land with AM15 |
| AM14b Action proof use case | OpenCode / DeepSeek deploy | `am14b-action-proof` | merged | `720fdc6`..`3902ecf`: trivial skip $0, worst-case budget bound before each call, reviewer tools bounded and path-guarded (`os.Root`, no `..`, no `.git`), Prove mapping, versioned artifact that the gate refuses on wrong head, base, policy hash, run, version, size, unknown field, bad counts or forged blocking flag (all give unknown). Lint 0, archcheck holds, unit 4950. Coordinator review: R3 made it copy the agent loop (`proof_loop.go`); AM14c puts one loop behind `ports.AgentLoop`. Not verified: live model, real sandbox (uidsandbox not merged), RecordProof not in Gate.Run |
| AM14c one agent loop behind a port | Claude / Sonnet (DeepSeek deploy failed with HTTP 400 before writing) | `am14c-loop` | running | also wires RecordProof into Gate.Run |
| P2.1c bench progress and parallel cases | OpenCode / DeepSeek deploy | `p2.1c-bench-speed` | running | live run is slow and silent (about 2.5 min per case) |
| t3code third-party comment dataset | OpenCode / DeepSeek V4.1 Flash free | `bench-t3code` | merged `32f5857` | 411 real bot comments from 30 merged pingdotgg/t3code PRs (read-only): Macroscope 175 (43% acted), Cursor Bugbot 135 (24% acted, 70% dismissed), CodeRabbit 60 (48% acted). Our parser keeps only 195 of 383: Macroscope, Codex and Devin unknown, Greptile marker missing, Copilot login `Copilot`, CodeRabbit Nov header, 145 outdated. Labels are a noisy proxy |
| AM7b parse real bot formats | OpenCode / DeepSeek V4.1 Flash (deploy) | `am7b-clean` | merged | 383 of 383 top-level t3code comments become candidates (was 196): Macroscope, Codex, Devin added, Greptile prose, both Copilot logins, CodeRabbit Nov header, outdated keep their anchor. Lint 0, unit 4930. Coordinator review: the delegate's branch `am7b-real-formats-t3` copied Cursor Bugbot deep-link tokens and ids from public comments (gitleaks 385 findings, flagged by the Stage 0 session); rebuilt as one commit `a5c6d8a` with 270 links and 135 ids REDACTED, gitleaks 0; out-of-scope PROGRESS and TRACKER edits dropped. The owner deleted the dirty branch and its worktree (Nov 21); gitleaks over all local branches: no leaks |
| Live Proof Bench run 1 | owner's terminal, DeepSeek V4.1 Flash on Deploy | `21a0a91` | done | recall 0/7, FP 0/2, 0 findings proposed in any case; 6 of 7 bug cases `no_submission` (agent loop stops on the first answer without a tool call); cobra clean hit an HTML 400 from Deploy; $0.86 at high stated prices, 84% of input from cache, p50 168 s per case. Not a quality score yet; transcripts not kept |
| AM12b robust agent loop | next, after AM14c | | waiting | tool_choice required, parse text tool calls, ask again on a text answer, bounded transcripts, HTTP status on refusals; then live run 2 on 2 cases |

## Merge into heimdall-build (owner yes, Nov 21 2026)

`product-build` at `35f7c6e` holds heimdall-build up to `f2c16ab`, no conflicts; on the merged tree lint 0 issues,
archcheck holds (109 packages), `make unit` 4326 tests. The Stage 0 session was asked (queued message) to merge
`product-build` into heimdall-build at its next clean point and run `make fast`; it reports back here on a conflict.
Not pushed.

## Owner decisions (Nov 21 2026: "go with your recommendations")

- ACTION_MODE Q1 to Q10 take the doc's recommendations; Q3 closed by S1: the ref `refs/heimdall/state`.
- AM5 decisions stand: not reproduced is unknown; the size rule counts test lines; an unknown mode is the strictest.
- Trivial globs: a team's list replaces the defaults (the stricter choice: a surprise costs proof runs, never a
  missed check). A4 setup prints the effective list.
- A9 sign-in matches GitHub by numeric user id and OIDC by `sub` by default (A9b running).
- Coverage: Cobertura moves to an adapter on `encoding/xml`; the domain allowlist is not widened.
- DeepSeek was out of implementation lanes (free limit, a paid run hung for 5 hours). Nov 21 afternoon: both Claude providers hit their limits and the owner said to use DeepSeek in OpenCode (deploy and free) instead of Sonnet. Delegates commit after every step; the coordinator reviews every DeepSeek diff before merging.

## Follow-ups and noticed

- Harness: T3 keeps a checkpoint ref per delegate turn under `refs/t3/orchestration-v2/checkpoints/` (287 on Nov 21). `make verify` runs `gitleaks git .` over all refs, so a bad delegate commit stays visible after its branch is deleted until its checkpoint ref is removed too (owner only, R11). Seen with AM7b `4ab3614`.

- AM6r for A2: `Prefix` can only strip. A Java report (`com/example/Foo.java` under `src/main/java/`), a Go module in a
  subdirectory and coverage.py run from a subdirectory all give Missing, which fails closed (unknown) but is a setup
  gap. A2 needs a path mapping (strip and add) in `action.diff_coverage`.
- AM6r: a Go line with one covered and one uncovered block counts as covered (hit counts add); blank lines inside a
  block count as instrumented. Go line coverage (93.0% for cobra) differs from `go tool cover` statements (86.8%);
  the summary must say "lines" so users do not compare the two.
- Naming clash (for the owner): fixtures sit in `internal/domain/coverage/testdata/real/` and
  `internal/adapters/coverage/testdata/real/` (the coordinator's brief chose the name). The bash guard matches the
  text `testdata/real` anywhere, the edit guard only the top-level folder. These are not the guarded top-level folder;
  renaming them is blocked by the guard, so the owner may rename them (for example to `testdata/oss/`) or leave them.

- AM8: `github.ActionAPI.GetRef` does not check that the ref points at a commit; the store fails on the next
  `GetCommit` with an untyped error. `ports/vcs_action.go` and `github/gitdata.go` still call the S1 PATCH finding
  "to be confirmed"; S1 confirmed it, so update those comments in the next GitHub adapter change.
- Harness: the stop hook checks the main checkout, so it blocks delegates in worktrees while another thread has Go
  edits open there (seen by S1 and AM8).

- AM7 for A5: the default `action.sources` Copilot login is `copilot-pull-request-reviewer[bot]`; newer APIs may say
  `Copilot`. Check the real login on a live PR before A5 ships, or Copilot comments are silently ignored.
- AM7 for AM14: the caller sets `Outdated` (REST `position` null or GraphQL `isOutdated`); LEFT-side comments stay
  unmapped with `Side` set; duplicate merging with our findings (6.2 step 5) is AM14's.

- AM14a for AM15: `ports.ReviewComment` carries no `position`, no `Outdated` and no `original_commit_id`, so the gate
  cannot pass the outdated flag the adapter can read. AM9's doc names `REST position null or GraphQL isOutdated` as the
  caller's job, but the port has no field for it. A comment whose current line is gone is still anchored through
  `original_line` when the adapter fills it; otherwise it drops as `no_line`. Decide whether AM15 adds the field or the
  adapter sets `Outdated` from `position` itself.
- AM14a for AM15: wiring needs two pieces this task leaves to `cmd`. `Config.Facts` must be built from
  `internal/secretscan`, the coverage adapter and the domain rules (`testintegrity`), and `Config.IsUnverified` must be
  `errors.Is(err, gitlog.ErrIntegrity)`. `Config.Resetter` needs a gitlog method that replaces a change's log; the store
  has none today, so a bad MAC fails closed until that exists.
- AM14a: `change.ExternalIngested.Side` accepts `external.SideLeft`, but the location of a LEFT comment is on the base
  file. The gate records it as given and the summary shows the path only, so no wrong head line becomes a comment.
- AM14a: the gate reads `heimdall.yaml` at the merge base with `ports.CodeReader` and decodes it with an injected
  `PolicyDecoder`. A real run should use `policyyaml.Decode`; the component test does, against the pay-demo fixture.

- AM9 for AM14: `Adapter.tokens` is the concrete App token source, so the existing PR, files, reviews and checks calls
  cannot use the Action's static token yet. Make the field the `Tokens` interface when AM14 wires Action mode.
- AM9 for AM8: reading the state ref needs `GetCommit`, recursive `GetTree` and `GetBlob`, which AM9 did not add.
- AM9: `ActionAPI.Comment` matches its own comment by marker and author login; `Adapter.Comment` matches any Bot. Pick
  one rule when the two paths meet in AM14.
- Flaky test: `localrunner TestTimeoutModuleKillsProcessGroup` failed once under load (`strconv.Atoi: parsing ""`),
  passed 3 of 3 alone. Stage 0 code; not touched here.

- AM6: run the parsers on real reports from open-source projects (Go cover profile, coverage.py Cobertura, a JS lcov) before A2. Queue with the Proof Bench real-repo work.
- Harness: `post-edit.sh` runs `go vet` in the main checkout with the worktree's absolute path, so it reports "no such file" on every edit in a worktree. Harmless but noisy; belongs to the Stage 0 session's harness.

- AM12: the loop does not cap a tool result's size; AM11 tools must bound their output. A tool that ignores its context can delay a cancel or deadline stop.
- AM5 decisions for the owner: "not reproduced" is unknown, not fail (blocks as action_required only when proof is
  required); the trivial size rule counts test lines too (stricter than section 7, which says outside tests); an
  unrecognised mode reads as enforce with proof required.
- AM5: `policy.Action.Effective()` replaces the trivial globs instead of adding to the defaults, so a team that lists
  one glob loses `docs/**` and `**/*.md`. Matches its doc comment, but a likely surprise for users; decide before A4.
- A9: GitHub sign-in matches the login name. GitHub logins can be renamed and later claimed by someone else, so the
  role map should hold the numeric user id (GET /user `id`) and show the login only. Same for OIDC: prefer `sub` over
  email where the issuer allows it.
- A9: sign-out revocations live in process memory. After a restart a signed-out cookie works until its absolute
  expiry (24 hours at most). Fine for one server; a shared store is needed before more than one replica.
- `go mod tidy -diff` is not clean on `product-build` before and after the A9 merge: `filippo.io/age` and
  `filippo.io/hpke` are required but no package imports them. Pre-existing; not changed here (R10).
