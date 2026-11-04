# Heimdall: MVP

Team: 4 developers, each with Claude Code on their own laptop
Event: Flip the Switch (WeMakeDevs x DevTools Academy x Switch), Nov 4 to 7, 2026
Submission deadline: Nov 7, 11:59 PM IST (internal target: Sat freeze at noon, submit by 4 PM IST)
Track: 01 Best Business Workflow ($2,000)
Status: MVP scope summary for people. Builders follow `BRAINSTORM_BACKLOG.md` Part 8 and `TECH_SPEC.md` (precedence D37). Pitch pack is in PRODUCT_PITCH.md.

> **Superseded here (Nov 3, Part 8):** chat app is Mattermost, not Slack (D19); spec and test agents are one test-author (D3); merge rule is spec 12.4, not the five conditions in section 8; humans sign off tests only in sensitive domains (D33); reviewers are picked by the author (D5) and run on the remote host (D43); section 12 streams are replaced by D62; section 14 spikes by Part 8's spike table. The rest of this doc still holds.

Name: **Heimdall** (D59). Tagline: **LGTM is not evidence.**

Long line: Heimdall automates engineering change assurance. It coordinates context-aware agents across development teams to write failure-first tests, independently review changes and establish evidence before code merges, requiring human intervention only when engineering judgment is needed.

---

## 1. The product in one minute

Heimdall makes every merge safe and definite.

- No LGTM. A PR cannot merge on a casual approval. The GitHub check goes green only when the tests pass, the slop check passes, every review finding is resolved with evidence, and a human approves.
- Anti-slop at the door. A huge PR with no linked ticket or no agreed behaviour is rejected before any reviewer spends time on it.
- Personal context. Each developer's own agent carries that developer's ownership, invariants and history, so the agent that reviews your code already knows what you know.
- Cross-team agents. A payments PR can pull in the security, infrastructure and consumer teams' agents. Each reviews from its own angle, inside a Switch room, under its own identity.

Two phases, one lifecycle:

- Workflow 1, requirement to implementation. A Jira ticket assigned to a human and moved to In Progress opens a Switch room (fallback: the developer says so in chat). The spec agent reads the ticket and asks up to three questions. The human agrees the behaviour. The test agent writes failing integration, contract and E2E tests. Only then does the developer's agent build.
- Workflow 2, PR to verified merge. The PR is raised and the gate runs. Reviewer agents are assembled, investigate with organizational memory, and file findings. The author's agent fixes them with evidence. Humans settle disagreements. The gate turns green and the PR merges.

Auto-fixing is not part of the MVP. The author's agent fixes findings, but nothing is auto-merged, and no LLM decides whether a merge is allowed. Agents establish the evidence; the gate (plain code) decides on that evidence.

Developer touchpoints (this is the zero-touch promise):
- Write the code (with their own agent).
- Answer questions the agents ask, in whichever chat they use (Mattermost by default, Microsoft Teams when connected; Slack and Telegram later).
- Nothing else. The dev does not start the tests, pick every reviewer, chase reviewers or file tickets. The approver (a human other than the author) signs off at the end, and only when the change needs judgment.

---

## 2. Who uses it

| Who | What they do in Heimdall | What they get |
|---|---|---|
| Developer (author) | Starts work in chat, answers spec questions, approves behaviour, asks reviewers, resolves findings | A PR that is safe to merge, with evidence |
| Reviewer (owner, security, infra, consumer team) | Gets asked into the room, reviews from their angle | Findings with proof, not opinions |
| Tech lead or approver | Settles disagreements, gives the final approval | A decision record in the room |
| Platform/SRE owner | Sets gate rules, owns the always-on machine | Rules that are plain code, auditable |

---

## 3. System map

Chat apps (Slack, Teams, Discord, Telegram, Mattermost)
  -> Switch rooms (one room per chat channel or work item)
     -> Agents with their own identities:
        - Spec agent (reads Jira, asks questions)
        - Test agent (writes failing tests)
        - Developer's agent (builds, fixes findings)
        - Reviewer agents (owner, security, infra, consumer)
        - Coordinator agent (assembles reviewers, tracks findings)
     -> Jira (requirements, acceptance criteria; status change to In Progress opens the room; rejects create tickets)
     -> Graphiti team memory on FalkorDB (temporal knowledge graph fed by Jira, GitHub, chat and decisions; queried through the heimdall knowledge tool)
     -> Always-on machine (Switch remote host; runs agents when laptops are closed)
  -> Gate service (plain code, posts GitHub check)
     -> GitHub (PRs, checks, merge)

The gate service is the only part that can turn the GitHub check green. Agents report; code decides.

---

## 4. Build or borrow

| Piece | Decision | Why |
|---|---|---|
| Chat rooms, agent identities, roles, remote hosts, history | Borrow from Switch | This is Switch's core and the hackathon's centrality criterion |
| Team knowledge layer | Borrow: Graphiti (Apache 2.0) on FalkorDB | Temporal knowledge graph (facts have valid/invalid times), hybrid search, team partitions, MCP server; light. Fallback: repo-local markdown memory |
| Personal context packs | Build | Small files per developer, approved by the developer, redacted before storage |
| Spec and test workflow | Build | This is the differentiator; it is our logic on top of Switch |
| Slop check and merge gate | Build | Deterministic code, small and testable |
| GitHub check integration | Build | Status check API, one webhook, one check name |
| Jira, GitHub and chat input | Our webhooks and Switch room reads feed memory as episodes | No separate crawler |
| Test runner | Borrow (the project's own runner) | We run the team's tests, we do not write a new one |

Memory note: the server scopes every memory query to the asker's teams (Graphiti group ids set server-side). Only confirmed facts count as evidence.

---

## 5. Workflows

### W0. Setup (once per developer, under 15 minutes)

1. Install the Heimdall skill (one command).
2. Log into Switch and enroll the laptop, or choose the always-on option.
3. Approve the personal context pack (the agent drafts it from CODEOWNERS, git history and the developer's notes; the developer edits and approves).
4. Connect GitHub, Jira and the chat app.

### W1. Requirement to failing tests

1. Trigger: a Jira ticket is assigned to a human and moved to In Progress. A Jira webhook creates a Switch room for the ticket and invites the assignee's agent. Other triggers: a GitHub branch-create webhook (branch name contains the ticket key, so the room opens the moment the dev starts), or the developer posts "working on PAY-142" in chat.
2. The spec agent reads the ticket and asks the knowledge agent for relevant team memory.
3. The spec agent asks at most three questions, in the room.
4. The developer and an approver answer and agree the behaviour. Agreed behaviour is recorded in the room.
5. The test agent writes integration, contract and E2E tests that fail for the right reason. Unit tests are written too, but they are secondary: the gate requires the integration, contract and E2E tests, and unit tests only count as supporting evidence. The tests are committed by the test agent to a protected path (CODEOWNERS plus branch protection) and the gate pins their commit hash, so the developer's agent cannot edit or delete them to get green. A change to them needs the approver's sign-off. These are the "independent CI tests": written from the spec, not from the implementation, and ready before the dev's code exists.
6. A human checks that the failing tests describe the agreed behaviour.
7. The developer's agent is handed the failing tests and builds until they pass.

Example tests for a refund endpoint:
- A refund cannot exceed the original payment.
- A repeated request cannot refund the same transaction twice.
- An unauthorized user cannot issue a refund.
- The response contract stays compatible with existing consumers.

### W2. PR to verified merge

1. The PR is opened or the developer runs `heimdall review`.
2. Slop check runs first (see section 8). If it fails, the PR is rejected with the reason and nothing else runs.
3. The gate checks that the W1 tests exist and pass.
4. Reviewers come from three sources, merged: the developer's named reviewers, the org policy file (`heimdall.yaml`: required agents by path, label or risk level), and CODEOWNERS as the default.
5. The coordinator opens a review room and each reviewer agent joins under its own identity.
6. Each reviewer agent pulls organizational memory and files findings. A finding has a claim, evidence (a test, a trace, a file and line, or a linked decision) and a severity.
7. The author's agent fixes findings. Each fix references the finding and the evidence.
8. Reviewers re-check their own findings. Disagreements go to a human in the room.
9. A human approves. The gate re-runs and turns the GitHub check green. The PR merges.
10. Reject path: if the slop check fails, or the PR is blocked past a time limit with unresolved findings, the gate keeps the check red, posts the reasons in the room and creates a Jira ticket (or moves the original ticket back to In Progress) with the failing rules and findings linked. Findings that are valid but out of scope become follow-up tickets. One ticket per rejection, never one per finding, to avoid ticket spam.

### W3. Review on request

A reviewer can be asked into any PR room, including one from a team they do not belong to. Membership is explicit. Linking two rooms does not give an agent access to the other room.

### W4. Owner offline

The developer's laptop is closed. The always-on snapshot of their agent can block a merge by filing findings, but it cannot approve alone. A human still approves. The remote host needs the repo, working directory and provider auth set up in advance; it is not a live copy of the laptop (spike S7).

### W5. Learning loop (stretch)

When a finding turns out to be wrong, the reviewer's invariant card is updated after a human confirms. Slack discussion is not a rule until a human confirms it.

---

## 6. Features

Priority: P0 must ship for the demo, P1 ships if time allows, P2 is later.

| ID | Feature | Priority |
|---|---|---|
| F1 | Start work from chat (spec agent triggered from a message) | P0 |
| F2 | Jira ticket fetch and linking | P0 |
| F3 | Spec questions capped at three, agreed behaviour recorded in the room | P0 |
| F4 | Test agent writes failing integration and contract tests | P0 |
| F5 | Red-first verification (tests must fail for the right reason) | P0 |
| F6 | Developer's agent builds against the failing tests | P0 |
| F7 | Slop check at the door | P0 |
| F8 | Gate service posts GitHub check | P0 |
| F9 | Gate rules as plain code (five merge conditions) | P0 |
| F10 | Reviewer assembly (explicit names or CODEOWNERS default) | P0 |
| F11 | Reviewer agents with angles (owner, security, infra, consumer) | P0 |
| F12 | Findings with claim, evidence and severity | P0 |
| F13 | Author's agent resolves findings with evidence | P0 |
| F14 | Human approval in room | P0 |
| F15 | Organizational memory via Graphiti (Jira, GitHub, chat, decisions) | P1 |
| F16 | Memory facts carry source and date; Slack is not a rule until confirmed | P1 |
| F17 | Always-on snapshot for offline owners (can block, cannot approve) | P1 |
| F18 | Personal context pack (ownership, invariants, incidents, conventions, unknowns) | P1 |
| F19 | Teams connection (P1, teammate sets it up, D79); Telegram (P2) | P1 |
| F20 | Learning loop and invariant card updates | P2 |
| F21 | Required CI/CD checks included in the gate (build, lint, security scan, deploy preview) | P0 |
| F22 | Jira webhook trigger: In Progress opens a room (chat message is the fallback) | P0 |
| F23 | Reject path: one Jira ticket per rejection, follow-up tickets for out-of-scope findings | P1 |
| F24 | Org policy file (`heimdall.yaml`) sets required reviewer agents by path, label or risk | P1 |
| F25 | Unit tests written as secondary evidence | P1 |
| F26 | Test lock: tests in a protected path, commit hash pinned by the gate | P0 |
| F27 | Branch-create webhook trigger (room opens when the dev starts) | P1 |
| F28 | Approval mode in org policy: `human` (default) or `auto_if_low_risk` | P2 |

---

## 7. Agents

| Agent | Job | Can block merge? |
|---|---|---|
| Spec agent | Reads ticket, asks up to three questions, records agreed behaviour | No |
| Test agent | Writes failing tests from agreed behaviour | No (its tests are the bar, through the gate) |
| Developer's agent | Builds; fixes findings with evidence | No |
| Owner reviewer | Domain invariants and business logic | Yes, while findings are unresolved |
| Security reviewer | Authentication, authorization, data exposure | Yes, while findings are unresolved |
| Infra reviewer | Deployment, configuration, resource impact | Yes, while findings are unresolved |
| Consumer reviewer | API compatibility, downstream dependencies | Yes, while findings are unresolved |
| Coordinator | Assembles reviewers (named, org policy, CODEOWNERS), tracks findings and status, files the reject ticket | No |
| Knowledge tool | Server-side tool that queries Graphiti with team scope and returns facts with source, validity dates and status | No |
| Gate service | Plain code; evaluates the five merge conditions | Yes (deterministic) |

Humans approve. No agent approves a merge.

---

## 8. Merge rules and slop check

### Five merge conditions (all must hold)

1. The PR links a Jira ticket and the agreed behaviour is approved by a human.
2. The W1 failing tests now pass, and the required CI/CD checks are green (build, lint, security scan, deploy preview, as configured). No test was deleted or weakened to make it pass.
3. The slop check passes.
4. Every finding is resolved with evidence (fixed with a test or diff, or dismissed with a reason that a reviewer signs).
5. A human approves in the room. The approver is not the author. Org policy has `approval: human` by default; `auto_if_low_risk` is a later option and is not in the MVP demo.

### Slop check (plain code)

- The PR links a ticket.
- Changed lines are under 400 (lockfiles excluded).
- Changed files match the files named in the agreed behaviour, or the PR explains why not.
- No edits outside the ticket's scope without a stated reason.
- Tests were added or changed.

A failing slop check rejects the PR with the failing rule. Reviewers are not pulled in.

Scope note: Heimdall gates the merge. Whether code is safe in prod also depends on the org's CI/CD checks, which the gate requires but does not replace. We do not claim to guarantee prod safety.

---

## 9. Connections

| Connection | Direction | How | MVP status | Fallback |
|---|---|---|---|---|
| Mattermost | Chat in, room out | Bundled and connected by Switch Console (V23) | P0 | None needed |
| Slack | Chat in, room out | Switch Slack app | P2 | Not in the demo |
| Teams | Chat in, room out | Switch Teams (Azure app registration, public listener via Cloudflare named tunnel), see `docs/TEAMS_SETUP.md` | P1 (D79) | Mattermost stays default |
| Telegram | Chat in, room out | Switch Telegram | P2 | Skip |
| Switch agents | Both | Switch protocol (HTTP, SSE, MCP) | P0 | None; this is the core |
| GitHub | PR events in, check out | Webhook plus Checks API | P0 | Manual `heimdall review` command |
| Jira | Webhook in (status to In Progress), ticket read, ticket create on reject | Jira webhook plus Atlassian MCP (mcp.atlassian.com/v2/mcp) | P0 trigger and read, P1 create | Chat trigger, ticket pasted into room, reject posted in room only |
| Graphiti + FalkorDB | Memory in and out | Episodes from webhooks and decisions; search via Graphiti MCP | P1 | Repo-local markdown memory |
| Always-on machine | Agents run remotely | Switch remote host or Railway template | P1 | Laptop only; document it |
| Test runner | Tests run | The project's own command | P0 | Fixed command in config |
| CI/CD | Check results in | GitHub required checks (Actions or other) | P0 | List required checks in config |

---

## 10. Knowledge layers and pack layout

### Two layers

- Personal layer: each developer's agent holds their context pack (ownership, invariants, incidents, conventions, unknowns).
- Team layer: Graphiti holds time-aware facts from Jira, GitHub, chat and decisions; agents query it through the heimdall knowledge tool.

Every fact keeps its source and date. A Slack message is recorded as a discussion. It becomes a rule only after a human confirms it.

### Personal pack layout

```
context-pack/
  ownership.yaml     # what I own, who I trust for what
  invariants.md      # rules that must hold in my code
  incidents.md       # past incidents and what they taught
  conventions.md     # how this team writes code
  unknowns.md        # what I do not know yet
```

Developer approves the pack. A redaction pass removes secrets, personal data and anything marked private before the pack is stored.

---

## 11. Demo story (seeded data)

Repo: `pay-demo` (small, seeded, public)
Jira: PAY-142, refund endpoint
Decision: ADR-004 "refunds are idempotent by Idempotency-Key" (accepted, so a confirmed memory fact), plus a chat thread that led to it (observed). Full scenario: backlog D57.
Earlier change: a `refund_ref` field was removed from a response. A consumer broke. Seeded as an incident note.

Demo sequence (about 90 seconds on screen):

1. In Jira: PAY-142 moves to In Progress. A room opens in Mattermost (fallback: the developer types "working on PAY-142").
2. The spec agent pulls PAY-142 and the idempotency decision. It asks one question: "Should a repeat request return the original refund or an error?" The developer answers. Behaviour is agreed.
3. The test agent writes failing tests. They fail for the right reason. Shown on screen.
4. The developer's agent builds. Tests go green.
5. A deliberately bad PR is opened: 1,400 lines, no ticket. The slop check rejects it in seconds. Shown on screen. A Jira ticket for the rejection is created (if S10 passes).
6. The good PR is opened. The payments, security and consumer agents join the room. The consumer agent cites the removed `refund_ref` incident. The security agent finds an authorization gap. The author's agent fixes it with a test.
7. A human approves. The GitHub check turns green.

Baseline for comparison: a generic reviewer on the same diff. It comments on style and misses the incident and the idempotency decision. We show both outputs side by side.

Honesty rule for the demo: every claim on screen must be real in the seeded repo. No faked agent output. If an agent fails live, we show the fallback.

---

## 12. Four-developer streams

| Stream | Owner | Scope |
|---|---|---|
| A. Switch and chat | Dev 1 | Room setup, Slack connection, agent identities and roles, always-on host |
| B. Workflow 1 | Dev 2 | Spec agent, test agent, red-first check, Jira fetch |
| C. Workflow 2 and gate | Dev 3 | Slop check, gate service, GitHub check, merge rules, reviewer assembly |
| D. Memory, seed and demo | Dev 4 | Graphiti setup (or repo memory fallback), seeded repo, Jira seed, baseline reviewer, one-command setup, README and video |

Each developer uses their own Claude Code. Shared contract: the gate's input format and the finding format are written down on Tuesday night and not changed without the others knowing.

---

## 13. Timeline

| Day | Date | Goal |
|---|---|---|
| Tue night | Nov 3 | Spikes S1 to S10 (section 14). Lock contracts for the finding and gate formats. |
| Wed | Nov 4 | Thin slice: chat message to spec question to one red test. |
| Thu | Nov 5 | W1 and W2 working end to end on the seeded repo, without memory. |
| Fri | Nov 6 | W3 and W4. Graphiti memory in (or fallback). Seeded data and baseline reviewer. One-command setup. |
| Sat morning | Nov 7 | Freeze at noon. Only bug fixes after that. Recorded video and submission text done. |
| Sat | Nov 7 | Submit by 4 PM IST (buffer; hard deadline 11:59 PM IST). |

---

## 14. Spikes (run Tuesday night; each has a fallback)

| Spike | Question | Fallback if it fails |
|---|---|---|
| S1 | Can a GitHub PR event reach a Switch room? | Gate posts a message to the room through a webhook; the room is updated by the coordinator |
| S2 | Can an agent read and write tasks and decisions in a room? | Store decisions in the room history as messages; agent reads history |
| S3 | Does Graphiti's MCP server ingest our episodes and return scoped facts (with a local embedder)? | Repo-local markdown memory with the same fact format |
| S4 | Can the gate post a GitHub check that blocks merge? | Required status check via branch protection; gate sets the status |
| S5 | Can one Switch room be used from two chat apps? | Demo uses Slack only; say so |
| S6 | Does the test agent produce tests that fail for the right reason on the seeded repo? | Hand-write two seeded tests and show the agent adapting them |
| S7 | Can an agent run on a Switch remote host with the repo and provider auth? | Demo on the laptop; show the remote host as architecture only |
| S8 | Does the Atlassian MCP fetch PAY-142 from the Jira seed? | Ticket text pasted into the room |
| S9 | Can a Jira status-change webhook create a Switch room and invite the assignee's agent? | Developer types the trigger in chat; coordinator opens the room |
| S10 | Can the gate create a Jira ticket (or move the ticket back to In Progress) through the Atlassian MCP with write access? | Reject reasons posted in the room only |

Rule: a spike result is written in the room notes on Tuesday night. No spike is assumed to work in the demo.

---

## 15. Judging map

| Criterion | Weight | How Heimdall answers it |
|---|---|---|
| Use of Switch | 30% | Chat-initiated work, rooms as the shared record, agents with identities and roles, reviewers on a remote host, decisions made in the room (D47) |
| Technical execution | 25% | Red-first tests, deterministic gate, GitHub check; **reproducible from a clone in local mode** (D38) |
| Creativity and impact | 25% | Tests before code, per-person context, cross-repo catch, evidence gate (different from plain review rooms, V29) |
| Presentation and documentation | 20% | Demo on seeded data, short video, README with the three required parts (V27) |

Weights confirmed on the DevTools Academy event page (V26).

---

## 16. Repo and README must-haves

- The three parts the rules require (V27): which agents we connected, how the channel is set up, how to run it.
- Local mode (D38): clone, `docker compose up`, Switch Console, one Anthropic key; no Jira, no public URL.
- One-command setup for the skill and for the gate.
- A 90-second demo GIF near the top.
- Seeded repos (`pay-demo`, `ledger-demo`) with the ticket seed, ADR-004 and INC-031 (D57).
- What is real and what is mocked, stated plainly.
- Workflow diagram (the W1 and W2 sequence).
- Merge rules and the slop check, as plain code with tests.
- Known limits: per-user memory permissions are not in the MVP; Telegram is not in the demo; Teams is shown if its connection passes its check; auto-fixing is not included.
- Credentials: the team's API keys and host credentials are configured by the user, never stored in the repo.
- License and attribution for Switch (Apache 2.0 with Commons Clause) Graphiti (Apache 2.0) and FalkorDB (SSPL, self-hosted).

---

## 17. Cut-lines

Cut in this order if time runs short:

1. F19 Telegram; Teams only if its connection is not ready by Friday 20:00.
2. F20 (learning loop) and F23 (Jira ticket on reject).
3. F17 (always-on snapshot). Demo on laptop.
4. F18 (personal context pack). Use CODEOWNERS only.
5. F16 (memory provenance display). Keep the source in the store, drop the UI.
6. F15 (Graphiti). Repo-local markdown memory.
7. LLM summary of findings. Show raw findings.

Never cut:
- Failing tests written before the build.
- The gate and the GitHub check.
- One cross-team finding with proof.
- Slop rejection.
- Starting from a trigger (Jira status change, or chat as the fallback).

---

## 18. Later (after the hackathon)

- Finer-grained memory permissions than team scope.
- Auto-fixing with human approval per fix.
- Invariant cards that update after human confirmation, with history.
- Mattermost, Teams and Telegram in production.
- Measurement: review time, reopened PRs, defects found after merge, against a control team.

---

## 19. Open items

- Closed items moved to `BRAINSTORM_BACKLOG.md` Part 8; only its "Still needs the team" list is open. The lines below are kept for history.
- Who pays for agent usage and the always-on machine during the hackathon, and who holds the credentials?
- Name clash check for "Heimdall" has not been done. Alternatives: Merge Room, Reviewroom, Greenlight, Gateroom.
- Confirm the remaining judging weights on the official page.
- Confirm that Switch's remote host can run with the team's repo and provider authentication (spike S7).
- Confirm Atlassian MCP access for the team's Jira site (spike S8).
- Decide who records the 3-minute video and when (Sat morning).
- Open design questions are tracked one by one in BRAINSTORM_BACKLOG.md (demo bug, agent deliberation, agent security, name, trust rules, scoped memory, test quality, approval policy, cost).
- Does the team's Jira site allow webhooks and write access for the demo, or do we use a free personal Jira site?
- Which CI/CD checks does the seeded repo require (build, lint, security scan), so F21 is real in the demo and not mocked?
- Reject timeout: how long can a PR sit blocked before the gate files the Jira ticket?
