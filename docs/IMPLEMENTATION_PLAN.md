# Heimdall: Implementation Plan

Source of truth for build order. The live tracker (shared page) holds status; this file holds the plan. Task ids are stable: use them in commits (`T23: event store append`) and PR titles.

Built from `TECH_SPEC.md` v3.6 and `BRAINSTORM_BACKLOG.md` (D36 build profile, D62 owners and gates; Parts 12 to 16 amend it). Tests and validation follow `TEST_STRATEGY.md`. Times are IST.

## Start here: Stage 1

**Before any product code, do `docs/STAGE_1_FIXES.md` in order.** It holds the human actions H1 to H6, the exact harness fixes (T100 to T104) with the checks already run on them, and the validations T01, T02 and T03. The docs already describe the corrected design, but the harness files are unchanged and no live agent has run. Live state is in `docs/PROGRESS.md`. A result that contradicts a decision changes the decision and the spec in the same commit (D134).

| Id | Task | Owner | Depends | Done when | Refs |
|---|---|---|---|---|---|
| T100 | Commit the corrected docs (done, branch `heimdall-build`) | Builder | H1 | One commit, `.env` still ignored | D138 |
| T101 | Replace `pre-bash-guard.sh`; add `test-hooks.sh` | Builder | T100 | Every case in `STAGE_1_FIXES.md` passes | D123 |
| T102 | Fix `golangci.yml` | Builder | T100 | `config verify` and the sample run match `STAGE_1_FIXES.md` | D123 |
| T103 | Add the permission allow list to `settings.json` | Builder | T100 | The file parses | D123 |
| T104 | Replace the OpenCode harness template | Builder | T100 | `opencode debug config` lists it; T03 passes | D141, D142 |

Then T01, T02, T03 in the G0 table below. Slice 0 starts only after that.

## Is the spec ready to build?

After Stage 1, yes, on two conditions:
1. **Interfaces become code before anyone else writes code.** The spec describes them; T11 turns them into Go types that everything imports (event catalog, MCP schemas, task envelope, policy schema, merge clauses). T01 to T03 run first because they can change them.
2. **Two skills are not written yet.** `skills/test-quality/SKILL.md` exists; the reviewer and resolver skills are tasks T40 and T41.

## Owners (D62)

| Dev | Area |
|---|---|
| Dev 1 | Switch, agents, VM, chat commands, deliberation |
| Dev 2 | W1: tracker, sandbox, test-author |
| Dev 3 | Core: event store, GitHub, gate, dashboard |
| Dev 4 | Seeds, packs, memory, skills, demo, docs |
| Teams friend | Teams connection |

**Executor note (D119).** Claude Code builds the product incrementally, one task at a time, so the Dev columns show the area, not parallel people. Vertical slices, the per-task loop and `docs/PROGRESS.md` (see `TEST_STRATEGY.md`) replace parallel hand-offs. The teammates own the VM, Teams, the OpenRouter keys and the demo recording.

## Gates

| Gate | Passes when |
|---|---|
| G0 Spikes | Each spike passes, or the failure and the chosen alternative are written in the tracker (the loser is deleted, D122) |
| G1 Skeleton | Module compiles; frozen interfaces merged; CI green |
| G2 Walking skeleton | `heimdall start <KEY>` in Mattermost opens a room; test-author gets a task; one seam test is red-verified, committed and pinned; a PR shows `heimdall/gate` pending |
| G3 Review loop | Full W2 with one reviewer on real Switch; gate red, then green after a GitHub approval on head |
| G4 Feature freeze | Three reviewers, memory (Graphiti), dispute card, injection PR, local mode on a clean machine, planted-issue check (2 of 3) |
| G5 Submit | A release is tagged and rehearsed; the demo recording and the submission are complete, before the hackathon's official deadline |

A missed gate triggers the cut order (spec section 21), never a skipped test.

**Gates are quality gates, not deadlines (D119).** Order matters and the clock does not: a gate passes when its evidence is green, however long that takes. The one fixed external date is the hackathon's official submission deadline. The VM exists, so the order is `MVP_SCENARIOS.md` section 7, and gates are checkpoints on it (D156).

**Evidence required to pass a gate** (all recorded in the tracker):

| Gate | Layers that must be green | Live LLM allowed |
|---|---|---|
| G1 | L0 to L3 in CI; `make drift` clean; `doctor` green | none |
| G2 | the above, plus L4 on the real stack with the scripted agent for W1; one L5 test-author smoke | about $0.10 |
| G3 | the above, plus L4 for W2 with all failure paths; L5 for reviewer and resolver; the adversarial review by a fresh session | about $0.40 |
| G4 | the above, plus chaos-lite, the injection eval, the planted-issue check (2 of 3), soak, local mode on a clean container | about $1.4 |
| G5 | a tagged release rehearsed on the VM; demo recording; budget ledger reconciled | demo key |

## Build order: vertical slices, judged by the prototype board

Build thin end to end first, then widen (`MVP_SCENARIOS.md`, section 7). The product's nine scenarios P1 to P9 run as `make prototype` and print a board of passing, failing and blocked steps; that board is the project's progress bar. Stage 1 comes first, then slices: 0 red board (T00, T10, T16, T18, T98), 1 the door (a real PR gets a real gate), 2 ticket to room, 3 room to red tests, 4 review and merge, 5 memory and packs, 6 live quality, 7 bad days and strangers, 8 ship. **`MVP_SCENARIOS.md` section 7 lists every task in order and is the one build order (D156)**; the tables below are grouped by gate only for reference. **A task is done only when its own tests pass and the board step it owns turned green** (infrastructure tasks name the first step they unblock). The board run before and after goes into the tracker note.

## How every task is verified

Follow `TEST_STRATEGY.md` and `MVP_SCENARIOS.md`. In short: write the failing test first and read why it fails, make it pass, run `make verify`, run the real check for that task (`make e2e T=<id>`), try to break it, then record the commands, results, live cost and anything not verified in the tracker and in `docs/PROGRESS.md`, and commit as `T##: ...`. A task is done only when all of that holds. The matrix in `TEST_STRATEGY.md` section 7 lists the first red test, the real check and the LLM cost for every task.

## Tasks

Status lives in the tracker. Columns: id, task, owner, gate, depends on, done when, spec refs.

### G0: spikes and accounts (throwaway)

| Id | Task | Owner | Depends | Done when | Refs |
|---|---|---|---|---|---|
| T01 | Spike S1/S11: install Node, OpenCode 1.x and the controller; coordinator registers, creates a room, invites an `owner-agents` agent, posts an addressed message, agent replies; chat command path; per-room sessions | Dev 1 | T97 | Checks a to g in `STAGE_1_FIXES.md` seen and recorded | V34, D61 |
| T02 | Spike S12: human identity by Switch sender id; agent posts map to no person | Dev 1 | T01 | The T02 checks in `STAGE_1_FIXES.md` pass | D47 |
| T03 | Spike S14: OpenCode harness from T104; per-agent token; `auto_mode`; nftables on the agents' group; real tool names recorded; attack check | Dev 1 | T01, T104 | The T03 checks in `STAGE_1_FIXES.md` pass; every attempt the attack check in CODING_STANDARDS section 15 requires to fail does fail | D44, D80 |
| T04 | Spike S4: GitHub App posts a check that branch protection requires | Dev 3 | H2, T31 | On `heimdall-fixture-pay`: PR merge button blocked by the check; what `doctor` can read about protection is recorded (D151) | 13 |
| T05 | Spike S6: seam test against a missing route fails as JUnit `<failure>` (not `<error>`) under both `go test` (gotestsum) and pytest | Dev 2 |  | XML shows `<failure>`, not `<error>` | D39 |
| T06 | Spike S13: Graphiti MCP + FalkorDB + hosted embeddings (`voyage-4-lite`, dims 1024, V50) + DeepSeek via OpenRouter with `response_format`; search filtered by `group_ids` | Dev 4 | T07 | Seeded fact returned for its group only | D66, D73 |
| T07 | Accounts: AWS VM, OpenRouter key, Jira KAN token, GitHub App, second GitHub identity `rekdfr` and SSH over SSM, all verified Nov 4 (D135). What is left is tracked as H2, H4 and H5 | All |  | Every account works; keys stored outside repos | D74, Part 8 open items |
| T09 | Spike S15 (optional, first in the cut order): Teams connection round trip (room on Teams bridge, untagged message reaches an agent, `heimdall …` parsed) | Teams friend | T07 | All three seen | D79, D81 |
| T91 | Spike S18: a container under `heimdall-runner` cannot reach the Docker socket, the metadata address, other containers or the internet; confirm the rootless runner isolates them | Dev 1 | T07, T105 | Escape tests all fail as intended | D91, V56 |
| T93 | Spike S20: a KAN ticket moved to In Progress is seen within one poll, a late-indexed move is seen through the overlap window, and nothing triggers twice | Dev 2 | T07 | Three KAN scenarios pass | D97, V55 |
| T94 | Spike S21: `heimdall doctor --host` on the real AWS VM reports sudo, systemd, Docker, nftables, IMDS and memory correctly | Dev 3 | T07, T105 | Report matches reality; no sudo is reported to the team as a blocker | D90 |
| T95 | Mattermost public access: named Cloudflare Tunnel, Site URL, Switch connection Public Url, invite-only, strong passwords, rate limit (`docs/MATTERMOST_ACCESS.md`) | Friend + Dev 1 | T07 | Teammates log in with the Mattermost app from their own network | D107 |
| T96 | Spike S22: phone on mobile data and a desktop on another network log in through the tunnel, see a room and get a live agent reply (WebSocket) | Dev 1 | T95 | Both clients pass; result recorded | D107 |
| T97 | Deploy Switch from its pinned standalone compose (`collab` and `gateway` profiles, `agent_management` flag, `AGENT_REGISTRATION_TOKEN`, restart policies, override for the Mattermost Site URL and `MESSAGING_PUBLIC_URL`); Console connects as a client through a port-forward; record the pinned version in the runbook | Dev 1 | T07 | The stack survives `sudo reboot` with rooms intact and Console shows the server | D124, D136 |

### G1: skeleton and frozen interfaces

| Id | Task | Owner | Depends | Done when | Refs |
|---|---|---|---|---|---|
| T00 | Toolchain on the build machine. Checked Nov 4: Docker works without sudo, `psql`, `gh`, `jq`, `make`, `opencode` present; **Go, gotestsum, staticcheck, golangci-lint, oasdiff and gitleaks are missing**. Install Go, pin the rest in a separate `tools.mod` and run them as `go tool -modfile=tools.mod <name>` so they stay out of the product's module (`STAGE_1_FIXES.md` T102); `make tools` fetches only what cannot be built that way, at a pinned checksum. Go tooling only, no Node | Dev 3 |  | `make tools` then `go version`, `gotestsum --version`, `golangci-lint --version`, `oasdiff --version` succeed; `docker run hello-world` works | TEST_STRATEGY 2 |
| T98 | Prototype board and user simulator (`e2e/`): `World` interface over Jira, GitHub and the chat, Story files for `fixture-pay` and `jurist`, scenarios P1 to P9 written as steps that all start red or `blocked`, `make prototype` and `make demo-check`, the product-quality checks (human words, reasons on every red, quiet, idempotent replay) | Dev 3 | T00, T10, T18 | `make prototype` prints the full board with every step red or blocked and an honest reason; a deliberately wrong product behaviour is caught by each quality check | MVP_SCENARIOS 2 to 6 |
| T10 | Go module at the root of the existing repo `flip_the_switch_hackathon` beside `docs/` (D120), layout, dependency-rule check, CI (vet, test, govulncheck), CLAUDE.md/AGENTS.md with precedence and the builder rules; README states the design docs predate the event (D61) | Dev 3 |  | CI green on empty skeleton | 2.2, D37 |
| T11 | Frozen interfaces as Go: event catalog structs, MCP tool schemas (`FindingIn`, `Review`, `TestSubmission`, `Verdict`), task envelope, policy schema + validation, merge-rule clause signatures, ports | Dev 3 | T10 | Merged; others import it | 3.2, 6.3, 14.1, 16, 12.4 |
| T12 | `docker compose` skeleton: server, Postgres, FalkorDB, Graphiti (Ollama only as an optional profile, D87); `heimdall doctor` stub | Dev 3 | T10 | `docker compose up` healthy | 1.3, D38 |
| T13 | Integration registry and provider selection from `heimdall.toml` | Dev 3 | T11 | A stub adapter is selectable by name with no change outside its package | 2.2, 16 |
| T14 | `heimdall connect` CLI (list, picker, secret-file instructions, `Check()`, `--json`) | Dev 3 | T13 | Mattermost, Jira and GitHub `connected`; `connect mattermost` validates the public address or accepts "internal only" (D109); Teams `needs config` | 16 |
| T15 | `heimdall-runner`: own user, rootless runtime, Unix socket job API (`run(profile, sha)` returns JUnit and a log blob); the server never holds the Docker socket | Dev 2 | T12, T91, T105 | Job runs; a metadata or socket access attempt fails | D91, 12.2 |
| T16 | `docs/CODING_STANDARDS.md` adopted; install the agent harness from `docs/harness/` (`make harness-install`: `AGENTS.md` with `CLAUDE.md` symlink replacing the bootstrap `CLAUDE.md`, `.claude` settings and hooks, `.golangci.yml` with `MODULE` filled in); `golangci-lint config verify` passes; `make lint-selftest` with a failing fixture per rule; `make harness-test`; LLD note template for each package | Dev 3 | T10 | CI fails on a violation of each rule; `make harness-test` passes every case in `STAGE_1_FIXES.md` | D104 |
| T16b | `make sandbox`: builder in a container, repo-only mount, egress allowlist, private Docker daemon. Required before the builder runs unattended with live credentials | Dev 3 | T16 | A sandboxed builder cannot read `~/.ssh` or `.env` | D104, D160 |
| T17 | Config and secrets loader (`secrets_dir`, `--config`, `[[projects]]`, `make secrets-local`), `/healthz` | Dev 3 | T10 | Server refuses to start on bad config; an unmapped project is logged once; a laptop run reads `secrets/` (D147, D148) | D89, D93, D102 |
| T18 | Test harness: `make fast`, `verify`, `drift`, `e2e` (a missing `TestTask_T##` fails), `eval`, `rehearse`, `soak`, `cost`, `harness-test`, `fixtures-push`; testcontainers; real-payload recorder (`make record`) and scrubber; pre-commit hook; `docs/PROGRESS.md` template; repo `CLAUDE.md` with the builder rules; `internal/testkit/` and the CI check that no production binary depends on it (D121) | Dev 3 | T10, T16 | `make fast` under 60 s and `make verify` runs in CI; a deliberately failing test blocks the commit | D111, D114 |
| T19 | LLM budget guard and cost ledger: per-purpose OpenRouter keys (test, eval, rehearse) with limits, balance check before any live run, committed ledger, stop rule at 2x estimate | Dev 3 | T07, T18 | `make e2e` refuses to start with too little balance; every live run adds a ledger line | D113 |

### G2: walking skeleton

| Id | Task | Owner | Depends | Done when | Refs |
|---|---|---|---|---|---|
| T105 | `make deploy`: build Linux binaries, copy over SSM SSH, install secrets to `/etc/heimdall/secrets`, `docker compose up -d` on loopback only; idempotent; `/mcp` on `127.0.0.1:8080` | Dev 3 | T12, T17 | Run twice changes nothing; the VM answers `/healthz`; an agent user can reach `/mcp` | D160 |
| T20 | Event store: append with expected version, UNIQUE version, update/delete triggers, hash chain, verify command, outbox, `cursor` table | Dev 3 | T11 | Conflict retried; chain verifies; `kill -9` mid-job loses nothing | 2.4, 4.1, D55 |
| T21 | Reducer and transition table with state × event table tests, including `UNLINKED_PR` and a PR before `BUILDING` (D146) | Dev 3 | T11 | Every illegal transition refused in tests | 3.3 |
| T22 | River queues, per-change advisory lock, outbox drainer with idempotent effect keys | Dev 3 | T20 | Duplicate effect never executes twice | 2.1, 2.4 |
| T23 | Step engine and Go-defined default workflow | Dev 3 | T21, T22 | Workflow runs on fakes end to end | 5, D54 |
| T24 | GitHub adapter, poll mode: PRs, comments, reviews, check runs; checks, branch, commit, tree hash, tarball; backoff | Dev 3 | T11, T04 | Pending `heimdall/gate` on a `heimdall-fixture-pay` PR; a PR with no ticket gets its own stream (D146) | 13, D64, D69 |
| T25 | Tracker port: file tracker and Jira JQL poller; AC parsing and ids | Dev 2 | T11 | File ticket and Jira ticket both create `change.triggered` | D50 |
| T26 | Switch client: register coordinator, rooms, invite, references, post, read_context; Switch stream with cursor and gap handling | Dev 1 | T01, T11 | Room opens from the engine; replies become events | 13, D46 |
| T27 | Agent runtime on the VM: make T01 and T03 repeatable in `admin init`: controller with `--separate-users`, self-update off, OpenCode pinned, agent definitions (`auto_mode: true`, D141), per-agent harness directories and tokens (D142), nftables on the agents' group and the metadata block | Dev 1 | T03, T105 | test-author runs under its own unit; it returns after `sudo reboot` | 6.9, D80 |
| T28 | MCP server (streamable HTTP + stdio bridge): auth by agent token, role checks, `get_task`, `get_change`, `ack`, `read_code`, `read_untrusted`, `submit_tests`, `ask_humans`, `report_progress` | Dev 1 | T11 | test-author calls tools from OpenCode | 14.1 |
| T29 | Sandbox: Go image, module cache, JUnit parser, red-first, flake (3 runs), seam import check | Dev 2 | T05, T12 | Import-error test rejected; assertion-red accepted | 12.1, 12.2, D39 |
| T30 | test-author flow: bundle, task, submission, quality checks, errors back to the agent within its budget, commit to `tests/heimdall/<KEY>`, pin that directory's tree hash (D150) | Dev 2 | T23, T28, T29 | Tests for every AC land on the branch and are pinned | 7, D45 |
| T31 | Hermetic fixtures `pay-demo` and `ledger-demo` (service, seam, OpenAPI, branches) in `testdata/repos/`, published as real repos by `make fixtures-push` (D149); the live target is Jurist AI (T75) | Dev 4 | H3 | `aarush323/heimdall-fixture-pay` is live; `good-pr`, `bad-pr`, `injection-pr` exist | 20, D57 |
| T32 | Room commands parser: `heimdall …` in chat and `/heimdall …` in GitHub comments, from mapped humans (D139), including `challenge` and `remember` | Dev 1 | T26 | `heimdall start <KEY>` works from Mattermost; a `!heimdall` post is documented as not reaching us; an agent's command post is refused | 11.5, D47, D81 |
| T34 | Repo profile, Go first: schema (with `acceptance`, `seam`, `ac_tag`, `fetch_hosts`, D153), detectors, validator (allowlisted image, limits, trial run emitting JUnit), profiler skill, PR proposal for profile and CI; replaces the fixed Go, Node and Python images in T29 | Dev 2 | T29, T15 | A Go fixture gets a valid profile through a PR | D98, D99, 12.2b |
| T35 | GitHub token source: `reader` and `writer(op, repo)` narrowed tokens, 50-minute cache; `doctor` reads the installation's real permissions | Dev 3 | T24 | A narrowed token cannot do anything outside its operation | D96, V51, V54 |
| T36 | Jira poll with overlap window and dedupe by (key, changelog id); ADF walker with golden tests on KAN tickets | Dev 2 | T25, T93 | KAN-1 to KAN-3 parse to the right AC; no double trigger | D97, V52 |
| T37 | `people:` mapping by Switch sender id (D140), filled by `heimdall connect people`; `github` optional except for authors and approvers (D158); text-command decisions bound to a mapped human and role | Dev 1 | T26, T32 | Wrong-person, agent-sender and stale-SHA attempts are refused in tests | D94, D95 |
| T38 | Scripted agent in `cmd/heimdall-testagent` (TEST-ONLY, never in a release build or image): a deterministic agent on the real Switch and MCP protocols returning canned valid and deliberately invalid submissions | Dev 1 | T26, T28 | Runs a whole W1 and W2 on the real stack at $0, including failure paths | D118 |
| T39 | Hermetic fixture repo matrix in `testdata/repos/`: Go, Python with Postgres, Node, small JVM, each with known red and green tests and a known profile | Dev 2 | T05 | Detectors and profiles right on every fixture | D116 |
| T34b | Jurist AI's repo profile through a PR: Postgres with pgvector on a services network, `fetch_hosts` for the spaCy model, FastAPI seam, `# heimdall:ACn` tags | Dev 2 | T34, T75 | Valid profile; a seam test goes red with an assertion in the sandbox | D153 |
| T75 | Jurist AI demo scenario on base branch `heimdall-demo` (never `main`, D154): ticket in KAN with acceptance criteria, a `good-pr` with planted issues, the frontend client as the cross-team fact, a `bad-pr` and an `injection-pr`; tests that need no model call | Dev 4 | T07, T39 | Scenario doc approved; each planted issue has a red test that catches it | D116, D101 |
| T33 | G2 demo run | All | T20 to T32, T34 to T39, T105 | Gate G2 passes | |

### G3: review loop

| Id | Task | Owner | Depends | Done when | Refs |
|---|---|---|---|---|---|
| T40 | Reviewer skill (`skills/reviewer/SKILL.md`) | Dev 4 | T11 | Skill produces schema-valid reviews on a sample diff | 6.4, 6.5 |
| T41 | Resolver skill (`skills/resolver/SKILL.md`) | Dev 4 | T11 | Skill produces a verdict with citations on a sample case | 11.3 |
| T42 | Context bundles for reviewer and resolver, stored as blobs with hash on the task | Dev 3 | T24 | Bundle hash recorded; replay gives identical bundle | D45 |
| T43 | Reviewer pick (command, room, default from CODEOWNERS), fan-out, one task per agent, ack then unavailable (D145) | Dev 1 | T27, T28, T32 | Three reviewers get tasks; a stopped agent is marked unavailable and can be waived | 8.1, 6.2, D80 |
| T44 | `submit_review`, output validation (paths, lines, refs), dedup, error feedback to the agent within its budget, output filter | Dev 1 | T28 | Invalid finding is returned with errors; it is rejected only if the agent runs out of budget | 6.5, 15.2 |
| T45 | Evidence verification: E1 repro, E2 `oasdiff` and `go vet`, E3 memory re-query; `heimdall_run_check`; tiers; blocking set; every push re-runs E1 and E2 and returns E3 and E4 on changed files to the filing reviewer (D152) | Dev 2 | T29 | Repro finding is E1 only when the runner confirms; a push with a real fix resolves it with no claim | 11.1 |
| T46 | Slop rule, with policy (`heimdall.yaml`) read from the merge base and fail-closed on invalid policy | Dev 3 | T24 | `bad-pr` red in under 15 s with the rule named (D155) | 12.3 |
| T47 | Merge rule, check publishing, SHA-bound approvals, test sign-off clause, per-ticket pin (D150) | Dev 3 | T45, T46 | Each clause has a flip test; real PR red → green on approved head | 12.4 |
| T48 | Pack builder: git, markdown, Claude Code transcripts; two redaction passes; review screen; `age` encrypt; upload; canary; `heimdall_read_pack` scoped to the caller (D156) | Dev 4 | T11 | Planted secret never reaches the pack; an agent reads only its own pack | 10, D68 |
| T49 | Memory: Graphiti compose, ontology, episode ledger (D51), `ask_knowledge` scoping and labels, seed backfill | Dev 4 | T06 | Consumer finding cites INC-031 as confirmed | 9, D51 |
| T50 | G3 demo run | All | T40 to T49 | Gate G3 passes | |

### G4: product experience, agent quality, then freeze

| Id | Task | Owner | Depends | Done when | Refs |
|---|---|---|---|---|---|
| T60 | Challenge rounds (`heimdall challenge` from chat and `heimdall_challenge`, D152), resolver case and `heimdall_submit_verdict`, recommendation on cards, consensus hold | Dev 1 | T44, T41, T32 | Dispute reaches resolver; human upholds from room; 2 high reporters hold the gate | 11.2b, 11.3, D40, D42 |
| T61 | Test sign-off for sensitive domains (`approve-tests`) | Dev 2 | T30, T32 | Money-movement ticket waits for approval before "build" is announced | D33 |
| T62 | Security: untrusted wrapping, injection heuristics, server-side injection invariants in CI on every push; the 12-payload suite against live agents as a budgeted eval (D115) | Dev 2 | T44 | Zero state changes from injections | 15.2, D60 |
| T63 | Read-only dashboard with SSE (changes, timeline, blockers, findings, open asks) | Dev 3 | T47 | Live updates during a full run | 14.3, D53 |
| T64 | Boot reconciliation | Dev 3 | T24, T25 | Stopped-VM events recovered after start | D56 |
| T65 | Chat memory: change-room digest at close; listed channels with keywords or `heimdall remember` | Dev 4 | T49 | Digest episode appears after merge | D65 |
| T66 | Baselines: CodeRabbit and plain DeepSeek reviewer on `good-pr`, saved verbatim | Dev 4 | T31 | Files in `seed/baseline/` | D58, D75 |
| T67 | Teams (optional, first in the cut order): `chat.bridge: teams`, thread replies for asks, `heimdall …` tested | Teams friend + Dev 1 | T09, T32 | A change room runs in Teams | D79, D81 |
| T68 | Local mode on a clean machine (D157): `admin init` deploys Switch and Heimdall, file tracker, polling, real agents under the controller, one OpenRouter key | Dev 3 | T63, T27, T99 | Red-then-green in under 20 min in a clean container with its own Docker daemon, no Jira, no public URL | D38 |
| T69 | Planted-issue check: real agents find each planted issue with verified evidence in at least 2 of 3 runs | All | T60 to T66 | 3 runs logged with the planted issues found per run | D60 |
| T71 | Reliability kit: `docs/RUNBOOK.md`, tagged-release deploys and `make recover`, backups, memory and cost guards, a demo recording, soak test with a PR every 5 minutes for 2 hours | Dev 3 | T63 | Soak passes; recover works from a stopped VM | D93, D102 |
| T99 | Guided setup: registry-declared steps and live checks in `heimdall connect` for Switch and Console, GitHub, Jira, OpenRouter, Mattermost and the agent controller; `admin init` deploys Switch. Telegram, Slack, Discord and Teams flows are optional and first in the cut order (D156) | Dev 3 | T14, T35 | Each integration can be completed from CLI prompts and resumed after stopping; a failed check names the fix; integrations without a real run show `untested` | D137, 17 |
| T72 | Product experience: `heimdall setup` and `heimdall connect` polish: platform picker, credential prompts that name the secret file, clear errors, `doctor` summary | Dev 3 | T14, T35 | A new user connects Mattermost, GitHub and Jira in under 15 minutes without reading the docs | D105, 17 |
| T73 | Agent quality: eval corpus run on Jurist AI, prompt and skill tuning for test-author, reviewer and profiler; compare against the plain DeepSeek reviewer | Dev 4 | T66 | Each planted issue found in at least 2 of 3 runs; baseline comparison saved | D60, D105 |
| T74 | Chaos-lite: kill server mid-step, kill runner, stop Postgres 30 s, pause Switch, flood sandbox queue, with the scripted agent | Dev 3 | T38, T71 | No lost events and no false green after each; recovery without manual steps | D117 |
| T70 | G4 freeze | All | T60 to T66, T68, T69, T71 to T74, T99 (core integrations) | Gate G4 passes | |

### G5: ship

| Id | Task | Owner | Depends | Done when | Refs |
|---|---|---|---|---|---|
| T80 | README: agents connected, channel setup (Mattermost and Teams), how to run, local mode, what is mocked, design docs written before the event | Dev 4 | T70 | A teammate follows it cold | V27, D61 |
| T81 | Demo video (team mode) and local-mode clip | Dev 4 | T70 | Under 3 min, captions | Pitch §4 |
| T82 | Blog post and #HowISwitch posts | Dev 4 | T70 | Published | Pitch §8, §9 |
| T83 | Submission text and form | All | T80, T81 | Submitted before the official deadline | V27 |
| T84 | Final `pg_dump` and tag the release | Dev 3 | T70 | Tag pushed | D55 |
