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
| AM8 git-ref event store | Claude Personal / Sonnet | `am8-gitlog` | held | interrupted at the usage limit with `internal/adapters/gitlog/` uncommitted. Held until S1 settles the compare-and-swap mechanism: REST `force:false` did not refuse a non-fast-forward update on `refs/heimdall/*` |
| AM6 coverage parsers | OpenCode / DeepSeek free; rework on Claude Personal / Sonnet | `am6-coverage` | rework | coordinator review of `11b7b5c`: (1) hand-written XML tokenizer in domain because depguard forbids `encoding/xml`; Cobertura moves to `internal/adapters/coverage` on `encoding/xml` behind a domain builder (deviation from the AM6 row, which put all three parsers in domain); (2) fail-open: a changed file absent from the report counted as not instrumented, so a wrong prefix gave 100%; adds `Missing` and `Measurable()`, caller treats not measurable as unknown. Free tier hit its limit at once; the paid Flash run hung about 5 hours and failed with partial edits (R12: two failures, moved to Sonnet) |
| AM10 model chat with tools | OpenCode / DeepSeek free | `am10-model-chat` | merged | reviewed by coordinator (cost math, bounded body, no key in errors); lint 0 issues, unit 3579 tests; no live model call |
| S1 GitHub spike | Claude Personal / Sonnet | `s1-github-spike` | running (resumed) | owner said yes Nov 21. First run interrupted at the usage limit after creating `heimdall-test/s1-k7*` branches on the fixture repo; the resumed run finishes and cleans up |
| AM5 proof outcome and trivial skip | Claude Personal / Sonnet | `am5-proof` | merged | delegate `e26824d`; coordinator review moved the duplicated Heimdall-file rule into one `policy.Action.Protects` used by both the fast check and the trivial skip (two copies could drift into a bypass), with a table test. Lint 0 issues, archcheck holds (105 packages), unit 3906 tests after merge. Real GitHub file lists (binary, rename) and AM14 integration not verified |
| AM12 agent loop | Claude Personal / Sonnet | `am12-agentloop` | merged | delegate `a9b75ad`; coordinator review added the section 7 per-run input and output token caps (stop as `StopBudget`) and made the per-answer output cap required. Lint 0 issues, archcheck holds (104 packages), unit 3776 tests after merge. No live model run, no real sandbox tools (AM11): not verified. AM5 must map `StopNoSubmission` and `StopModelError` |
| AM3 Action events | waiting | | blocked | needs Stage 0 to release `change` |
| A9b sign-in by stable id | Claude Personal / Sonnet | `a9b-stable-id` | running | |
| AM7 third-party comment parsers | Claude Personal / Sonnet | `am7-external` | running | |
| AM9 GitHub adapter additions | Claude Personal / Sonnet | `am9-github` | running | no CAS claimed in `gitdata`; AM8 owns the conflict mechanism after S1 |

## Owner decisions (Nov 21 2026: "go with your recommendations")

- ACTION_MODE Q1 to Q10 take the doc's recommendations; Q3 waits on S1.
- AM5 decisions stand: not reproduced is unknown; the size rule counts test lines; an unknown mode is the strictest.
- Trivial globs: a team's list replaces the defaults (the stricter choice: a surprise costs proof runs, never a
  missed check). A4 setup prints the effective list.
- A9 sign-in matches GitHub by numeric user id and OIDC by `sub` by default (A9b running).
- Coverage: Cobertura moves to an adapter on `encoding/xml`; the domain allowlist is not widened.
- DeepSeek is out of implementation lanes for now (free limit, a paid run hung for 5 hours); Sonnet takes them.

## Follow-ups and noticed

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
