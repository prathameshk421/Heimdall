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
| AM8 git-ref event store | Claude Personal / Opus | `am8-gitlog` | running (restarted) | on S1 1.7: create by REST POST, update only by GraphQL `updateRefs` with `beforeOid`, reread the tip after an opaque error; old pre-S1 code committed as WIP first, then reworked |
| AM6 coverage parsers | OpenCode / DeepSeek free; rework on Claude Personal / Sonnet | `am6-coverage` | merged | rework `ab75685`: Cobertura in `internal/adapters/coverage` on `encoding/xml` (Strict, depth 32, size checked first, one external DOCTYPE allowed because coverage.py and Java Cobertura write it, internal subsets refused); domain `Builder`; `Missing` and `Measurable()` fail closed. Fuzz 30 s each, 3M+ execs, pass. Lint 0 issues, archcheck holds (107 packages), unit 4060 tests after merge. Real reports beyond one golden and the gate wiring (AM14) not verified |
| AM10 model chat with tools | OpenCode / DeepSeek free | `am10-model-chat` | merged | reviewed by coordinator (cost math, bounded body, no key in errors); lint 0 issues, unit 3579 tests; no live model call |
| S1 GitHub spike | Claude Personal / Sonnet | `s1-github-spike` | merged `1d31cfd` | `52ed487`, `ACTION_MODE_SPIKE.md`: REST `force:false` is no CAS on `refs/heimdall/*` (stale, rewind, unrelated all 200); GraphQL `updateRefs` with `beforeOid` is a real CAS with `GITHUB_TOKEN`; Q3 closed: the ref (clones skip it, writes start no workflows); comment API took 262,144 bytes (docs say 65,536); blob 10 MiB ok; push to check p50 6.1 s, p95 8.8 s (n=10); cancel cut at 300 s. `workflow_run` facts blocked: need a default-branch workflow, an environment and a fork from `rekdfr`, each a separate owner yes. Cleanup verified; closed PR #17 and two check runs cannot be deleted by API |
| AM5 proof outcome and trivial skip | Claude Personal / Sonnet | `am5-proof` | merged | delegate `e26824d`; coordinator review moved the duplicated Heimdall-file rule into one `policy.Action.Protects` used by both the fast check and the trivial skip (two copies could drift into a bypass), with a table test. Lint 0 issues, archcheck holds (105 packages), unit 3906 tests after merge. Real GitHub file lists (binary, rename) and AM14 integration not verified |
| AM12 agent loop | Claude Personal / Sonnet | `am12-agentloop` | merged | delegate `a9b75ad`; coordinator review added the section 7 per-run input and output token caps (stop as `StopBudget`) and made the per-answer output cap required. Lint 0 issues, archcheck holds (104 packages), unit 3776 tests after merge. No live model run, no real sandbox tools (AM11): not verified. AM5 must map `StopNoSubmission` and `StopModelError` |
| AM3 Action events | waiting | | blocked | needs Stage 0 to release `change` |
| A9b sign-in by stable id | Claude Personal / Sonnet | `a9b-stable-id` | merged | `2d96eb1`: GitHub by numeric `id` (int64, zero, negative, text, float, huge refused), OIDC `sub` by default, no login fallback, config refuses a GitHub person without `github_id`, session identity re-checked per request in constant time, old cookies fail to open. Break attempt: guard mutated, test caught it. Lint 0 issues, archcheck holds, unit 4129 after merge. Live provider sign-in not verified. Operators: GitHub configs need `github_id`; email OIDC needs `login_claim = "email"`; everyone signs in once after deploy |
| AM7 third-party comment parsers | Claude Personal / Sonnet | `am7-external` | merged | `3b68d20`: CodeRabbit, Copilot, Cursor Bugbot, Greptile by Bot type + login in `sources` + body mark; mapping through diff hunks reviewed by coordinator; typed drops; bounds in doc.go. Fuzz pass, 64 KiB pathological bodies under 200 ms. Lint 0 issues, archcheck holds (108 packages), unit 4210 after merge. Goldens hand-made from documented formats: real bot comments not verified (A5) |
| AM11 isolated sandbox + escape probes | Claude Personal / Opus | `am11-sandbox` | running | owner yes ("go with everything") for throwaway runs on the fixture repo, S1 guardrails |
| AM9 GitHub adapter additions | Claude Personal / Sonnet | `am9-github` | merged | `40b792d`: static token, comments by marker and author, artifact zip (streaming cap, entry count, per-entry limit read through, path escape, symlink and duplicate refusal), code scanning, Git Data (no CAS claimed). New ports in `internal/ports/vcs_action.go`. Coordinator kept 422 untyped: the shared adapter's test asserts it on purpose and the live gate uses it; AM8 calls GetRef instead. Lint 0 issues, unit 4109 after merge. Live GitHub calls, cross-host artifact redirect, comment size limit not verified |

## Owner decisions (Nov 21 2026: "go with your recommendations")

- ACTION_MODE Q1 to Q10 take the doc's recommendations; Q3 closed by S1: the ref `refs/heimdall/state`.
- AM5 decisions stand: not reproduced is unknown; the size rule counts test lines; an unknown mode is the strictest.
- Trivial globs: a team's list replaces the defaults (the stricter choice: a surprise costs proof runs, never a
  missed check). A4 setup prints the effective list.
- A9 sign-in matches GitHub by numeric user id and OIDC by `sub` by default (A9b running).
- Coverage: Cobertura moves to an adapter on `encoding/xml`; the domain allowlist is not widened.
- DeepSeek is out of implementation lanes for now (free limit, a paid run hung for 5 hours); Sonnet takes them.

## Follow-ups and noticed

- AM7 for A5: the default `action.sources` Copilot login is `copilot-pull-request-reviewer[bot]`; newer APIs may say
  `Copilot`. Check the real login on a live PR before A5 ships, or Copilot comments are silently ignored.
- AM7 for AM14: the caller sets `Outdated` (REST `position` null or GraphQL `isOutdated`); LEFT-side comments stay
  unmapped with `Side` set; duplicate merging with our findings (6.2 step 5) is AM14's.

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
