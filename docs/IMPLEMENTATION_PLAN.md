# Heimdall: Implementation Plan

Source of truth for build order. The live tracker (shared page) holds status; this file holds the plan. Task ids are stable: use them in commits (`T23: event store append`) and PR titles.

Built from `TECH_SPEC.md` v3.4 and `BRAINSTORM_BACKLOG.md` (D36 build profile, D62 owners and gates). Times are IST.

## Is the spec ready to build?

Yes, with three conditions:
1. **Spikes first.** Six facts are still unverified (S1/S12, S4, S6, S13, S14, S15). Each has a fallback in the spec, but a failed spike changes a day's plan, so they run first, in throwaway code (D61).
2. **Interfaces become code before anyone else writes code.** The spec describes them; T10 turns them into Go types that everyone imports (event catalog, MCP schemas, task envelope, policy schema, merge clauses).
3. **Two skills are not written yet.** `skills/test-quality/SKILL.md` exists; the reviewer and resolver skills are tasks T40 and T41.

## Owners (D62)

| Dev | Area |
|---|---|
| Dev 1 | Switch, agents, VM, chat commands, deliberation |
| Dev 2 | W1: tracker, sandbox, test-author |
| Dev 3 | Core: event store, GitHub, gate, dashboard |
| Dev 4 | Seeds, packs, memory, skills, demo, docs |
| Teams friend | Teams connection |

## Gates

| Gate | When | Passes when |
|---|---|---|
| G0 Spikes | Tue night | Each spike passes or its fallback is chosen and written in the tracker |
| G1 Skeleton | Wed 10:00 | Module compiles; frozen interfaces merged; CI green |
| G2 Walking skeleton | Wed 20:00 | `/heimdall start PAY-142` in Mattermost opens a room; test-author gets a task; one seam test is red-verified, committed and pinned; a PR shows `heimdall/gate` pending |
| G3 Review loop | Thu 20:00 | Full W2 with one reviewer on real Switch; gate red, then green after a GitHub approval on head |
| G4 Feature freeze | Fri 20:00 | Three reviewers, memory (Graphiti or `pgmem`), dispute card, injection PR, local mode on a clean machine, D60 live check 3 of 3 |
| G5 Submit | Sat 16:00 | Code freeze 12:00, video 15:00, submitted 16:00 |

A missed gate triggers the cut order (spec section 21), never a skipped test.

## Tasks

Status lives in the tracker. Columns: id, task, owner, gate, depends on, done when, spec refs.

### G0: spikes and accounts (Tue night, throwaway)

| Id | Task | Owner | Depends | Done when | Refs |
|---|---|---|---|---|---|
| T01 | Spike S1/S11: coordinator registers, creates a room, invites an `owner-agents` agent, posts an addressed message, agent replies | Dev 1 | T07 | Reply seen in Mattermost | V34, D61 |
| T02 | Spike S12: human vs agent posts distinguishable by sender fields; human id stable | Dev 1 | T01 | Two posts logged with different `sender_kind`, same human id twice | D47 |
| T03 | Spike S14: OpenCode harness denies bash/webfetch; uid nftables blocks GitHub; `heimdall_*` MCP call works; session reset clears context | Dev 1 | T07 | All four checks pass, or fallback written | D44, D80 |
| T04 | Spike S4: GitHub App posts a check that branch protection requires | Dev 3 | T07 | PR merge button blocked by the check | 13 |
| T05 | Spike S6: seam test against a missing route fails as JUnit `<failure>` under gotestsum | Dev 2 | | XML shows `<failure>`, not `<error>` | D39 |
| T06 | Spike S13: Graphiti MCP + FalkorDB + hosted embeddings (`voyage-4-lite`, dims 1024, V50) + DeepSeek via OpenRouter with `response_format`; search filtered by `group_ids` | Dev 4 | T07 | Seeded fact returned for its group only | D66, D73 |
| T07 | Accounts: AWS VM (limited login from a friend; check sudo) with Budgets alerts, OpenRouter key with limit, GitHub App and repos, Jira site, Mattermost accounts for bob, dana, ana, eli, alice. Done Nov 4: OpenRouter key ($3 test cap), Jira KAN token, GitHub App installed on the account. Open: AWS VM, Mattermost users, role map | All | | Every account works; keys stored outside repos | D74, Part 8 open items |
| T08 | Spike S16: does Hugging Face Inference Providers serve DeepSeek V4 Flash (or a similar tool-calling model); price; credit expiry | Dev 4 | | Answer written in the tracker | D83 |
| T09 | Spike S15: Teams connection round trip (room on Teams bridge, untagged message reaches an agent, `!heimdall` parsed) | Teams friend | T07 | All three seen | D79, D81 |
| T90 | Spike S17: on the real remote host, `!run-cmd` and Switch's agent controller do not fight our systemd units; write the rule | Dev 1 | T07 | Rule written; agents survive a restart and a reboot | D92 |
| T91 | Spike S18: a container under `heimdall-runner` cannot reach the Docker socket, the metadata address, other containers or the internet; pick gVisor or the rootless fallback | Dev 1 | T07 | Escape tests all fail as intended; choice recorded | D91, V56 |
| T92 | Spike S19: capability probe on Mattermost and Teams (threads, cards, mentions); text-command approval works on both | Dev 1 | T01 | Table of capabilities filled; `!heimdall approve` works | D94 |
| T93 | Spike S20: a KAN ticket moved to In Progress is seen within one poll, a late-indexed move is seen through the overlap window, and nothing triggers twice | Dev 2 | T07 | Three KAN scenarios pass | D97, V55 |
| T94 | Spike S21: `heimdall doctor --host` on the real AWS VM reports sudo, systemd, Docker, nftables, IMDS and memory correctly | Dev 3 | T07 | Report matches reality; fallback chosen if no sudo | D90 |
| T95 | Mattermost public access: named Cloudflare Tunnel, Site URL, Switch connection Public Url, invite-only, MFA, rate limit (`docs/MATTERMOST_ACCESS.md`) | Friend + Dev 1 | T07 | Teammates log in with the Mattermost app from their own network | D107 |
| T96 | Spike S22: phone on mobile data and a desktop on another network log in through the tunnel, see a room and get a live agent reply (WebSocket) | Dev 1 | T95 | Both clients pass; result recorded | D107 |

### G1: skeleton and frozen interfaces (Wed 00:01 to 10:00)

| Id | Task | Owner | Depends | Done when | Refs |
|---|---|---|---|---|---|
| T10 | Repo `heimdall`: Go module, layout, dependency-rule check, CI (vet, test, govulncheck), `docs/` copied, CLAUDE.md/AGENTS.md with precedence | Dev 3 | | CI green on empty skeleton | 2.2, D37 |
| T11 | Frozen interfaces as Go: event catalog structs, MCP tool schemas (`FindingIn`, `Review`, `TestSubmission`, `Verdict`), task envelope, policy schema + validation, merge-rule clause signatures, ports | Dev 3 | T10 | Merged; others import it | 3.2, 6.3, 14.1, 16, 12.4 |
| T12 | `docker compose` skeleton: server, Postgres, FalkorDB, Graphiti (Ollama only as an optional profile, D87); `heimdall doctor` stub | Dev 3 | T10 | `docker compose up` healthy | 1.3, D38 |
| T13 | Integration registry and provider selection from `heimdall.toml` | Dev 3 | T11 | A stub adapter is selectable by name with no change outside its package | 2.2, 16 |
| T14 | `heimdall connect` CLI (list, picker, secret-file instructions, `Check()`, `--json`) | Dev 3 | T13 | Mattermost, Jira and GitHub `connected`; Teams `needs config` | 16 |
| T15 | `heimdall-runner`: own user, rootless runtime, Unix socket job API (`run(profile, sha)` returns JUnit and a log blob); the server never holds the Docker socket | Dev 2 | T12, T91 | Job runs; a metadata or socket access attempt fails | D91, 12.2 |
| T16 | `docs/CODING_STANDARDS.md` adopted; strict `golangci-lint` config; dependency-rule check in CI; LLD note template for each package | Dev 3 | T10 | CI fails on a violation of each rule | D104 |
| T17 | Config and secrets loader (`/etc/heimdall/secrets`), `/healthz`, kill switches (`memory`, `resolver`, `chat.cards`, `gate.mode`) | Dev 3 | T10 | Server refuses to start on bad config; each switch has a test | D89, D93, D102 |

### G2: walking skeleton (Wed to 20:00)

| Id | Task | Owner | Depends | Done when | Refs |
|---|---|---|---|---|---|
| T20 | Event store: append with expected version, UNIQUE version, update/delete triggers, hash chain, verify command, outbox, `cursor` table | Dev 3 | T11 | Conflict retried; chain verifies; `kill -9` mid-job loses nothing | 2.4, 4.1, D55 |
| T21 | Reducer and transition table with state × event table tests | Dev 3 | T11 | Every illegal transition refused in tests | 3.3 |
| T22 | River queues, per-change advisory lock, outbox drainer with idempotent effect keys | Dev 3 | T20 | Duplicate effect never executes twice | 2.1, 2.4 |
| T23 | Step engine and Go-defined default workflow | Dev 3 | T21, T22 | Workflow runs on fakes end to end | 5, D54 |
| T24 | GitHub adapter, poll mode: PRs, comments, reviews, check runs; checks, branch, commit, tree hash, tarball; backoff | Dev 3 | T11, T04 | Pending `heimdall/gate` on a `pay-demo` PR | 13, D64, D69 |
| T25 | Tracker port: file tracker and Jira JQL poller; AC parsing and ids | Dev 2 | T11 | File ticket and Jira ticket both create `change.triggered` | D50 |
| T26 | Switch client: register coordinator, rooms, invite, references, post, read_context; Switch stream with cursor and gap handling | Dev 1 | T01, T11 | Room opens from the engine; replies become events | 13, D46 |
| T27 | Agent runtime on the VM: per-agent Unix user, harness dir, systemd unit from `!run-cmd`, nftables per uid; `admin init` step | Dev 1 | T03 | test-author runs under its own unit | 6.9, D80 |
| T28 | MCP server (streamable HTTP + stdio bridge): auth by agent token, role checks, `get_task`, `ack`, `read_code`, `read_untrusted`, `submit_tests`, `ask_humans` | Dev 1 | T11 | test-author calls tools from OpenCode | 14.1 |
| T29 | Sandbox: Go image, module cache, JUnit parser, red-first, flake (3 runs), seam import check | Dev 2 | T05, T12 | Import-error test rejected; assertion-red accepted | 12.1, 12.2, D39 |
| T30 | test-author flow: bundle, task, submission, quality checks, retries, commit to `tests/heimdall/<KEY>`, pin tree hash | Dev 2 | T23, T28, T29 | Tests for every AC land on the branch and are pinned | 7, D45 |
| T31 | Seed `pay-demo` (service, seam, OpenAPI, tickets, branches) and `ledger-demo` | Dev 4 | T07 | Repos public; `good-pr`, `bad-pr`, `injection-pr` exist | 20, D57 |
| T32 | Room commands parser (`/heimdall` and `!heimdall`) from GitHub comments and mapped humans in rooms | Dev 1 | T26 | `start PAY-142` works from Mattermost | 11.5, D47, D81 |
| T34 | Repo profile: schema, detectors, validator (allowlisted image, limits, trial run emitting JUnit), profiler skill, PR proposal for profile and CI; replaces the fixed Go, Node and Python images in T29 | Dev 2 | T29, T15 | Jurist AI gets a valid profile through a PR, including its Postgres service | D98, D99, 12.2b |
| T35 | GitHub token source: `reader` and `writer(op, repo)` narrowed tokens, 50-minute cache; `doctor` reads the installation's real permissions | Dev 3 | T24 | A narrowed token cannot do anything outside its operation | D96, V51, V54 |
| T36 | Jira poll with overlap window and dedupe by (key, changelog id); ADF walker with golden tests on KAN tickets | Dev 2 | T25, T93 | KAN-1 to KAN-3 parse to the right AC; no double trigger | D97, V52 |
| T37 | Chat capabilities on the `Chat` port, `people:` mapping, text-command approvals bound to a mapped human, role and SHA | Dev 1 | T26, T32, T92 | Wrong-person and stale-SHA approvals are refused in tests | D94, D95 |
| T33 | G2 demo run | All | T20 to T32 and T34 to T37 | Gate G2 passes | |

### G3: review loop (Thu to 20:00)

| Id | Task | Owner | Depends | Done when | Refs |
|---|---|---|---|---|---|
| T40 | Reviewer skill (`skills/reviewer/SKILL.md`) | Dev 4 | T11 | Skill produces schema-valid reviews on a sample diff | 6.4, 6.5 |
| T41 | Resolver skill (`skills/resolver/SKILL.md`) | Dev 4 | T11 | Skill produces a verdict with citations on a sample case | 11.3 |
| T42 | Context bundles for reviewer and resolver, stored as blobs with hash on the task | Dev 3 | T24 | Bundle hash recorded; replay gives identical bundle | D45 |
| T43 | Reviewer pick (command, room, default from CODEOWNERS), fan-out, one task per agent, ack/restart/unavailable, reset after task | Dev 1 | T27, T28, T32 | Three reviewers get tasks; a stopped agent is restarted once then marked unavailable | 8.1, 6.2, D80 |
| T44 | `submit_review`, output validation (paths, lines, refs), dedup, one repair attempt, output filter | Dev 1 | T28 | Invalid finding repaired once, then rejected | 6.5, 15.2 |
| T45 | Evidence verification: E1 repro, E2 `oasdiff` and `go vet`, E3 memory re-query; tiers; blocking set | Dev 2 | T29 | Repro finding is E1 only when the runner confirms | 11.1 |
| T46 | Slop rule | Dev 3 | T24 | `bad-pr` red in under 10 s with the rule named | 12.3 |
| T47 | Merge rule, check publishing, SHA-bound approvals, test sign-off clause | Dev 3 | T45, T46 | Each clause has a flip test; real PR red → green on approved head | 12.4 |
| T48 | Pack builder: git, markdown, Claude Code transcripts; two redaction passes; review screen; `age` encrypt; upload; canary | Dev 4 | T11 | Planted secret never reaches the pack; agent decrypts only its own pack | 10, D68 |
| T49 | Memory: Graphiti compose, ontology, episode ledger (D51), `ask_knowledge` scoping and labels, seed backfill | Dev 4 | T06 | Consumer finding cites INC-031 as confirmed | 9, D51 |
| T50 | G3 demo run | All | T40 to T49 | Gate G3 passes | |

### G4: product experience, agent quality, then freeze (Fri to 20:00)

| Id | Task | Owner | Depends | Done when | Refs |
|---|---|---|---|---|---|
| T60 | Challenge rounds, resolver recommendation on cards, consensus hold | Dev 1 | T44, T41 | Dispute reaches resolver; human upholds from room; 2 high reporters hold the gate | 11.2b, 11.3, D40, D42 |
| T61 | Test sign-off for sensitive domains (`approve-tests`) | Dev 2 | T30, T32 | Money-movement ticket waits for approval before "build" is announced | D33 |
| T62 | Security: untrusted wrapping, injection heuristics, 12-payload injection suite in CI | Dev 2 | T44 | Zero state changes from injections | 15.2, D60 |
| T63 | Read-only dashboard with SSE (changes, timeline, blockers, findings, open asks) | Dev 3 | T47 | Live updates during a full run | 14.3, D53 |
| T64 | Boot reconciliation | Dev 3 | T24, T25 | Stopped-VM events recovered after start | D56 |
| T65 | Chat memory: change-room digest at close; listed channels with keywords or `!remember` | Dev 4 | T49 | Digest episode appears after merge | D65 |
| T66 | Baselines: CodeRabbit and plain DeepSeek reviewer on `good-pr`, saved verbatim | Dev 4 | T31 | Files in `seed/baseline/` | D58, D75 |
| T67 | Teams: `chat.bridge: teams`, thread replies for asks, `!heimdall` tested | Teams friend + Dev 1 | T09, T32 | A change room runs in Teams | D79, D81 |
| T68 | Local mode on a clean machine (poll, file tracker, compose) | Dev 3 | T63 | Red-then-green in under 20 min, no Jira, no public URL | D38 |
| T69 | D60 live check: real agents give the same blocking set 3 of 3 | All | T60 to T66 | 3 identical runs logged | D60 |
| T71 | Reliability kit: `docs/RUNBOOK.md`, `make deploy` and `make recover`, backups, memory and cost guards, fallback video, soak test with a PR every 5 minutes for 2 hours | Dev 3 | T63 | Soak passes; recover works from a stopped VM | D93, D102 |
| T72 | Product experience: `heimdall setup` and `heimdall connect` polish: platform picker, credential prompts that name the secret file, clear errors, `doctor` summary | Dev 3 | T14, T35 | A new user connects Mattermost, GitHub and Jira in under 15 minutes without reading the docs | D105, 17 |
| T73 | Agent quality: eval corpus run on Jurist AI, prompt and skill tuning for test-author, reviewer and profiler; compare against the plain DeepSeek reviewer | Dev 4 | T66 | Blocking set stable 3 of 3; baseline comparison saved | D60, D105 |
| T70 | G4 freeze | All | T60 to T73 | Gate G4 passes | |

### G5: ship (Sat)

| Id | Task | Owner | Depends | Done when | Refs |
|---|---|---|---|---|---|
| T80 | README: agents connected, channel setup (Mattermost and Teams), how to run, local mode, what is mocked, design docs written before the event | Dev 4 | T70 | A teammate follows it cold | V27, D61 |
| T81 | Demo video (team mode) and local-mode clip | Dev 4 | T70 | Under 3 min, captions | Pitch §4 |
| T82 | Blog post and #HowISwitch posts | Dev 4 | T70 | Published | Pitch §8, §9 |
| T83 | Submission text and form | All | T80, T81 | Submitted by 16:00 | V27 |
| T84 | Final `pg_dump` and tag the release | Dev 3 | T70 | Tag pushed | D55 |
