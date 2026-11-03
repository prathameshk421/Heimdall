# Proofroom Build Tracker

Live status for `IMPLEMENTATION_PLAN.md` (the plan holds the details, refs and gate definitions). Edit this file in the repo and commit; one line per change keeps merges easy.

**How to update:** change the Status cell, put your name in Who, and add a short note (blocker, PR link, fallback chosen). Use task ids in commits: `T23: event store append`.

**Status values:** `todo` · `doing` · `blocked` · `done`. A task is ready when every task in Depends is `done`.

## Summary

| Gate | When | Done | Total |
|---|---|---|---|
| G0 spikes and accounts | Tue night | 0 | 9 |
| G1 skeleton and frozen interfaces | Wed 10:00 | 0 | 3 |
| G2 walking skeleton | Wed 20:00 | 0 | 14 |
| G3 review loop | Thu 20:00 | 0 | 11 |
| G4 feature freeze | Fri 20:00 | 0 | 11 |
| G5 ship | Sat 16:00 | 0 | 5 |
| **All** | | **0** | **53** |

## Who is who

| Role | Person | Area |
|---|---|---|
| Dev 1 | | Switch, agents, VM, chat commands, deliberation |
| Dev 2 | | W1, tracker, sandbox, test-author |
| Dev 3 | | Core, GitHub, gate, dashboard |
| Dev 4 | | Seeds, packs, memory, skills, demo, docs |
| Teams friend | | Teams connection |

## Blockers

| Task | Blocked by | Since | Owner of the fix |
|---|---|---|---|
| | | | |

## G0: spikes and accounts (Tue night, throwaway)

| Status | Id | Task | Owner | Who | Depends | Done when | Note |
|---|---|---|---|---|---|---|---|
| todo | T01 | Spike S1/S11: coordinator registers, creates a room, invites an `owner-agents` agent, posts an addressed message, agent replies | Dev 1 |  | T07 | Reply seen in Mattermost |  |
| todo | T02 | Spike S12: human vs agent posts distinguishable by sender fields; human id stable | Dev 1 |  | T01 | Two posts logged with different `sender_kind`, same human id twice |  |
| todo | T03 | Spike S14: OpenCode harness denies bash/webfetch; uid nftables blocks GitHub; `proofroom_*` MCP call works; session reset clears context | Dev 1 |  | T07 | All four checks pass, or fallback written |  |
| todo | T04 | Spike S4: GitHub App posts a check that branch protection requires | Dev 3 |  | T07 | PR merge button blocked by the check |  |
| todo | T05 | Spike S6: seam test against a missing route fails as JUnit `<failure>` under gotestsum | Dev 2 |  | – | XML shows `<failure>`, not `<error>` |  |
| todo | T06 | Spike S13: Graphiti MCP + FalkorDB + Ollama `qwen3-embedding:0.6b` (dims 1024) + DeepSeek via OpenRouter with `response_format`; search filtered by `group_ids` | Dev 4 |  | T07 | Seeded fact returned for its group only |  |
| todo | T07 | Accounts: GCP project with budget alerts, OpenRouter keys per role with limits, GitHub org and repos, Jira site, Mattermost accounts for bob, dana, ana, eli, alice | All |  | – | Every account works; keys stored outside repos |  |
| todo | T08 | Spike S16: does Hugging Face Inference Providers serve DeepSeek V4 Flash (or a similar tool-calling model); price; credit expiry | Dev 4 |  | – | Answer written in the tracker |  |
| todo | T09 | Spike S15: Teams connection round trip (room on Teams bridge, untagged message reaches an agent, `!proofroom` parsed) | Teams friend |  | T07 | All three seen |  |

## G1: skeleton and frozen interfaces (Wed 00:01 to 10:00)

| Status | Id | Task | Owner | Who | Depends | Done when | Note |
|---|---|---|---|---|---|---|---|
| todo | T10 | Repo `proofroom`: Go module, layout, dependency-rule check, CI (vet, test, govulncheck), `docs/` copied, CLAUDE.md/AGENTS.md with precedence | Dev 3 |  | – | CI green on empty skeleton |  |
| todo | T11 | Frozen interfaces as Go: event catalog structs, MCP tool schemas (`FindingIn`, `Review`, `TestSubmission`, `Verdict`), task envelope, policy schema + validation, merge-rule clause signatures, ports | Dev 3 |  | T10 | Merged; others import it |  |
| todo | T12 | `docker compose` skeleton: server, Postgres, FalkorDB, Graphiti, Ollama; `proofroom doctor` stub | Dev 3 |  | T10 | `docker compose up` healthy |  |

## G2: walking skeleton (Wed to 20:00)

| Status | Id | Task | Owner | Who | Depends | Done when | Note |
|---|---|---|---|---|---|---|---|
| todo | T20 | Event store: append with expected version, UNIQUE version, update/delete triggers, hash chain, verify command, outbox, `cursor` table | Dev 3 |  | T11 | Conflict retried; chain verifies; `kill -9` mid-job loses nothing |  |
| todo | T21 | Reducer and transition table with state × event table tests | Dev 3 |  | T11 | Every illegal transition refused in tests |  |
| todo | T22 | River queues, per-change advisory lock, outbox drainer with idempotent effect keys | Dev 3 |  | T20 | Duplicate effect never executes twice |  |
| todo | T23 | Step engine and Go-defined default workflow | Dev 3 |  | T21, T22 | Workflow runs on fakes end to end |  |
| todo | T24 | GitHub adapter, poll mode: PRs, comments, reviews, check runs; checks, branch, commit, tree hash, tarball; backoff | Dev 3 |  | T11, T04 | Pending `proofroom/gate` on a `pay-demo` PR |  |
| todo | T25 | Tracker port: file tracker and Jira JQL poller; AC parsing and ids | Dev 2 |  | T11 | File ticket and Jira ticket both create `change.triggered` |  |
| todo | T26 | Switch client: register coordinator, rooms, invite, references, post, read_context; Switch stream with cursor and gap handling | Dev 1 |  | T01, T11 | Room opens from the engine; replies become events |  |
| todo | T27 | Agent runtime on the VM: per-agent Unix user, harness dir, systemd unit from `!run-cmd`, nftables per uid; `admin init` step | Dev 1 |  | T03 | test-author runs under its own unit |  |
| todo | T28 | MCP server (streamable HTTP + stdio bridge): auth by agent token, role checks, `get_task`, `ack`, `read_code`, `read_untrusted`, `submit_tests`, `ask_humans` | Dev 1 |  | T11 | test-author calls tools from OpenCode |  |
| todo | T29 | Sandbox: Go image, module cache, JUnit parser, red-first, flake (3 runs), seam import check | Dev 2 |  | T05, T12 | Import-error test rejected; assertion-red accepted |  |
| todo | T30 | test-author flow: bundle, task, submission, quality checks, retries, commit to `tests/proofroom/<KEY>`, pin tree hash | Dev 2 |  | T23, T28, T29 | Tests for every AC land on the branch and are pinned |  |
| todo | T31 | Seed `pay-demo` (service, seam, OpenAPI, tickets, branches) and `ledger-demo` | Dev 4 |  | T07 | Repos public; `good-pr`, `bad-pr`, `injection-pr` exist |  |
| todo | T32 | Room commands parser (`/proofroom` and `!proofroom`) from GitHub comments and mapped humans in rooms | Dev 1 |  | T26 | `start PAY-142` works from Mattermost |  |
| todo | T33 | G2 demo run | All |  | T20 to T32 | Gate G2 passes |  |

## G3: review loop (Thu to 20:00)

| Status | Id | Task | Owner | Who | Depends | Done when | Note |
|---|---|---|---|---|---|---|---|
| todo | T40 | Reviewer skill (`skills/reviewer/SKILL.md`) | Dev 4 |  | T11 | Skill produces schema-valid reviews on a sample diff |  |
| todo | T41 | Resolver skill (`skills/resolver/SKILL.md`) | Dev 4 |  | T11 | Skill produces a verdict with citations on a sample case |  |
| todo | T42 | Context bundles for reviewer and resolver, stored as blobs with hash on the task | Dev 3 |  | T24 | Bundle hash recorded; replay gives identical bundle |  |
| todo | T43 | Reviewer pick (command, room, default from CODEOWNERS), fan-out, one task per agent, ack/restart/unavailable, reset after task | Dev 1 |  | T27, T28, T32 | Three reviewers get tasks; a stopped agent is restarted once then marked unavailable |  |
| todo | T44 | `submit_review`, output validation (paths, lines, refs), dedup, one repair attempt, output filter | Dev 1 |  | T28 | Invalid finding repaired once, then rejected |  |
| todo | T45 | Evidence verification: E1 repro, E2 `oasdiff` and `go vet`, E3 memory re-query; tiers; blocking set | Dev 2 |  | T29 | Repro finding is E1 only when the runner confirms |  |
| todo | T46 | Slop rule | Dev 3 |  | T24 | `bad-pr` red in under 10 s with the rule named |  |
| todo | T47 | Merge rule, check publishing, SHA-bound approvals, test sign-off clause | Dev 3 |  | T45, T46 | Each clause has a flip test; real PR red → green on approved head |  |
| todo | T48 | Pack builder: git, markdown, Claude Code transcripts; two redaction passes; review screen; `age` encrypt; upload; canary | Dev 4 |  | T11 | Planted secret never reaches the pack; agent decrypts only its own pack |  |
| todo | T49 | Memory: Graphiti compose, ontology, episode ledger (D51), `ask_knowledge` scoping and labels, seed backfill | Dev 4 |  | T06 | Consumer finding cites INC-031 as confirmed |  |
| todo | T50 | G3 demo run | All |  | T40 to T49 | Gate G3 passes |  |

## G4: feature freeze (Fri to 20:00)

| Status | Id | Task | Owner | Who | Depends | Done when | Note |
|---|---|---|---|---|---|---|---|
| todo | T60 | Challenge rounds, resolver recommendation on cards, consensus hold | Dev 1 |  | T44, T41 | Dispute reaches resolver; human upholds from room; 2 high reporters hold the gate |  |
| todo | T61 | Test sign-off for sensitive domains (`approve-tests`) | Dev 2 |  | T30, T32 | Money-movement ticket waits for approval before "build" is announced |  |
| todo | T62 | Security: untrusted wrapping, injection heuristics, 12-payload injection suite in CI | Dev 2 |  | T44 | Zero state changes from injections |  |
| todo | T63 | Read-only dashboard with SSE (changes, timeline, blockers, findings, open asks) | Dev 3 |  | T47 | Live updates during a full run |  |
| todo | T64 | Boot reconciliation | Dev 3 |  | T24, T25 | Stopped-VM events recovered after start |  |
| todo | T65 | Chat memory: change-room digest at close; listed channels with keywords or `!remember` | Dev 4 |  | T49 | Digest episode appears after merge |  |
| todo | T66 | Baselines: CodeRabbit and plain DeepSeek reviewer on `good-pr`, saved verbatim | Dev 4 |  | T31 | Files in `seed/baseline/` |  |
| todo | T67 | Teams: `chat.bridge: teams`, thread replies for asks, `!proofroom` tested | Teams friend + Dev 1 |  | T09, T32 | A change room runs in Teams |  |
| todo | T68 | Local mode on a clean machine (poll, file tracker, compose) | Dev 3 |  | T63 | Red-then-green in under 20 min, no Jira, no public URL |  |
| todo | T69 | D60 live check: real agents give the same blocking set 3 of 3 | All |  | T60 to T66 | 3 identical runs logged |  |
| todo | T70 | G4 freeze | All |  | T60 to T69 | Gate G4 passes |  |

## G5: ship (Sat)

| Status | Id | Task | Owner | Who | Depends | Done when | Note |
|---|---|---|---|---|---|---|---|
| todo | T80 | README: agents connected, channel setup (Mattermost and Teams), how to run, local mode, what is mocked, design docs written before the event | Dev 4 |  | T70 | A teammate follows it cold |  |
| todo | T81 | Demo video (team mode) and local-mode clip | Dev 4 |  | T70 | Under 3 min, captions |  |
| todo | T82 | Blog post and #HowISwitch posts | Dev 4 |  | T70 | Published |  |
| todo | T83 | Submission text and form | All |  | T80, T81 | Submitted by 16:00 |  |
| todo | T84 | Final `pg_dump` and tag the release | Dev 3 |  | T70 | Tag pushed |  |

## Spike results (fill in G0)

| Spike | Task | Result | Fallback chosen |
|---|---|---|---|
| S1/S11 | T01 | | |
| S12 | T02 | | |
| S14 | T03 | | |
| S4 | T04 | | |
| S6 | T05 | | |
| S13 | T06 | | |
| S16 (HF credits) | T08 | | |
| S15 (Teams) | T09 | | |
