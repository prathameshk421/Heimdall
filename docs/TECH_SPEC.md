# Heimdall: Technical Spec (v3)

Status: v3.6, Nov 4, 2026. Name `heimdall` (final, D59). Language: Go. v3.6 applies backlog Part 16 (D138 to D156), the final review before development: `docs/STAGE_1_FIXES.md` runs first and proves the Switch facts it relies on. v3.5 applies backlog Part 12 (D86 to D106): AWS instead of GCP, hosted embeddings instead of Ollama, a separate sandbox runner, a generic repo profile, narrowed GitHub tokens, capability-driven chat and the hackathon reliability rules (0.5). v3.4 applies backlog Parts 9 to 11 (D63 to D85): a full consistency review, agent supervision (6.9), Teams in scope; OpenRouter DeepSeek models on OpenCode, no Anthropic API; reviewer agents on the VM only, no ingress (polling), chat history in memory, verified Graphiti config.
Companion docs: `HEIMDALL_MVP.md` (scope), `PRODUCT_PITCH.md` (story), `TECH_DESIGN.md` (principles, v0.1), `CODING_STANDARDS.md` (code rules, D104), `TEST_STRATEGY.md` (tests, validation and cost, D111), `BRAINSTORM_BACKLOG.md` (every fact `V#` and decision `T#`/`D#`/`U#`/`G#` cited here). Precedence (D138): among backlog Parts 8 and later the highest-numbered part wins, then this spec, then earlier backlog parts, MVP, TECH_DESIGN and the pitch.

**Build for Saturday: read section 0.4 first.** This spec is the product design; section 0.4 says which parts are built this week.

**What we are building.** A harness written in Go, plus a set of pluggable agents. A Jira ticket becomes failure-first tests in CI. The PR goes to reviewers the author picked, and each reviewer's agent brings that person's context. Disagreements are settled by evidence, then by a resolver, then by humans. A plain-code gate decides the merge. All of it is visible in chat (through Switch) and on a dashboard.

---

## 0. Goals, non-goals, quality bars

### 0.1 Goals

1. **No rubber-stamp merges.** A merge requires evidence-backed reviews, green failure-first tests and a human approval bound to the exact commit.
2. **Context-rich review.** Reviewers are people's own agents carrying their distilled context, plus shared team memory with time-aware facts.
3. **Spec before code.** Tests come from the ticket before the implementation exists.
4. **Low human load.** People answer questions, pick reviewers and make judgment calls. Agents and code do the rest.
5. **A real product.** One-command setup, multi-runtime agents (Claude Code, Codex, OpenCode), and enough auditability, security and operability to pilot with a real team.

### 0.2 Non-goals (MVP)

- Auto-merge, or auto-fix without a human. GitHub merges, not us.
- Replacing CI/CD. We add one required job and one required check.
- Multi-tenant SaaS. One deployment per organization.
- A fixed language list. Any repo a profile can describe works (D98); parsers ship for `go test`, `pytest`, `jest`/`vitest` and Maven/Gradle, and any other runner must emit JUnit XML.

### 0.3 Quality bars (one team VM)

| Metric | Target |
|---|---|
| Webhook acknowledged | p99 < 200 ms (verify, persist, enqueue only) |
| Slop decision on GitHub after PR open | p95 < 15 s (a 10 s poll plus the decision, D155) |
| Gate re-evaluation after a relevant event | p95 < 2 s |
| Tests ready after In Progress (no questions) | p50 < 15 min |
| All reviews in after reviewer pick | p50 < 12 min; per-reviewer timeout 30 min, then `unavailable` (waivable) |
| Dashboard page | p95 < 300 ms server time |
| Concurrent active changes | 50 |
| `heimdall-server` RSS idle / 50 active changes | < 80 MB / < 400 MB |
| Data loss on crash | none for accepted events (persist before ack) |
| False block rate on the seed corpus | tracked; target < 10% |

Section 18's load test validates these numbers. They are product targets, not hackathon gates (D36).

### 0.4 Hackathon build profile (D36)

The build is the IN column of D36 in `BRAINSTORM_BACKLOG.md` Part 8. In short:
- **Built:** event store with outbox and hash chain (D55), Go-defined workflow (D54), W1 with the seam rule (D39), W2 with three reviewer agents on the remote host (D43), evidence tiers E1 to E3, challenge rounds, resolver as recommendation only (D42), consensus hold (D40), merge rule and check, per-role agent harness (D44), context bundles (D45), coordinator stream (D46), Graphiti memory with guard rails (D51), packs with redaction, encryption and canaries, read-only dashboard (D53), local mode (D38), boot reconciliation (D56).
- **Not built this week:** webhooks (poll only, D160), `claim_fix` (D160), resolver auto-apply, live/twin split, mutation testing, YAML workflow loader, OAuth dashboard, signed chain heads, rebuild and restore commands, OTel and Prometheus, the k6 load test (a chaos-lite set and the soak are built, D117), testing the setup skill on Claude Code and Codex laptops (OpenCode is the VM runtime and is tested, D72), the full eval corpus (D60 defines the smaller set).

Sections below that describe only Later behaviour are marked **(Later)** where they would otherwise mislead a builder.

### 0.5 Hackathon reliability rules (D102, D103)

Nothing may break during the demo, so every dependency has a way to degrade and every release is rehearsed.
- **Pin everything:** the Switch version, every image by digest, vendored Go modules for deploy builds, the OpenCode version.
- **One command to deploy and one to recover** (`make deploy`, `make recover`), idempotent and rehearsed. Deploy only tagged releases, and never during recording.
- **Rehearse:** the planted issues are found with verified evidence in at least 2 of 3 runs on the tagged build (D122), plus a 2-hour soak with a PR opened every 5 minutes.
- **No escape hatches in the product.** The gate fails closed, and there is no advisory mode and no emergency feature switches (D121). If the gate is wrong during the demo, an admin toggles the branch protection rule by the logged break-glass in `docs/RUNBOOK.md`.
- **Backups:** `pg_dump` of both databases and an EBS snapshot before each demo day. A recorded fallback video of a clean run before the freeze.
- **Guards:** `docker stats` and `free -m` logged every minute with an alarm at 85% memory; the OpenRouter spend is read from the key endpoint before and after every eval run (the test cap is $3). AWS spend is capped at $50 (D110), and `heimdall doctor` prints an estimate from instance uptime because billing data lags by hours.
- **Cut order, used only when the owner explicitly says so (D162):** Teams, guided setup for Slack, Discord and Telegram, the infra role, chat memory, the resolver, Graphiti, Jira (file tracker). No builder cuts scope on its own. **Never cut** the event store, the merge rule and check, sandbox isolation, the GitHub App, Mattermost rooms, failure-first tests and text-command approvals.
- **Owner sequencing decision (Nov 5, 2026):** this is a sequencing decision, not a scope cut. First prove the core setup and Jira → tests → CI → reviewer → gate workflow with Mattermost/Switch, GitHub, Jira, OpenRouter and the agent controller. Defer Telegram, Slack, Discord and Teams connection planning and implementation until that workflow passes; handle them in a separate G7 planning session (T111) and implementation (T112). Do not treat a user's separate bots as Heimdall integration evidence.

### 0.6 Two design rules (D121, D122)

1. **Test scaffolding never ships.** Scripted agents, doubles, recorders and fixtures live only in `internal/testkit/`, `cmd/heimdall-testagent/` and `_test.go` files. No production package imports them, the release build and images exclude them, and production code has no test-mode flags or branches. CI fails the build if a production binary depends on `testkit`.
2. **Constrain safety, not thought.** Code is deterministic where correctness or security needs it: the merge decision, evidence verification (code runs the repro), permissions, SHA-bound approvals, the sandbox, and a minimal valid shape for what an agent submits. The model is free everywhere judgment is needed: how a reviewer reasons and explores, how the test-author designs tests, how the resolver weighs a dispute. No forced temperature or seed, no fixed number of attempts, no exact-output expectations. Quality is judged by outcomes over a few runs. And no fallback exists unless a real failure needs one: a spike decides between options, and the option not chosen is deleted, not kept as a hidden second path.

---

## 1. Architecture

### 1.1 Context

```mermaid
flowchart TB
  subgraph Humans
    DEV[Developer]
    APR[Approver]
    ADM[Admin]
  end
  subgraph SaaS
    JIRA[Jira Cloud]
    GH[GitHub]
    LLM[OpenRouter: DeepSeek V4 Flash, one production key]
  end
  subgraph VM[Team VM]
    PS[heimdall-server, Go]
    PG[(Postgres: events, projections, jobs)]
    BL[(Blob volume)]
    SB[Sandbox containers]
    GR[Graphiti MCP server]
    FK[(FalkorDB)]
    EM[Hosted embeddings via OpenRouter: voyage-4-lite, 1024 dims]
    SW[Switch server + Mattermost; Teams via its own public listener]
    VA[VM agents: test-author, resolver, one reviewer per person]
  end
  subgraph Laptops
    LA[Developer's own coding agent; builds the pack at setup]
  end
  PS -- polls JQL every 30s --> JIRA
  PS -- polls every 10s --> GH
  PS -- checks, commits, comments --> GH
  PS -- tickets --> JIRA
  PS -- coordinator ops --> SW
  SW <--> VA
  VA -- MCP tools --> PS
  LA -- MCP tools --> PS
  PS -- MCP client --> GR
  GR --> FK
  GR --> EM
  GR -- extraction --> LLM
  VA -- model calls --> LLM
  PS --> PG
  PS --> BL
  PS --> SB
  DEV & APR & ADM -- browser --> PS
  DEV & APR -- Mattermost or Teams --> SW
```

### 1.2 Planes

| Plane | Carries | Authority |
|---|---|---|
| Conversation (Switch rooms in Mattermost or Microsoft Teams; Slack and Telegram later) | Questions, milestones, decision cards | None. Free text never changes state |
| Control (MCP tools, authenticated webhooks, dashboard actions) | Schema-validated submissions, authenticated human decisions | Changes state through commands only |
| Memory (Graphiti) | Time-aware team facts | Advisory. Only `confirmed` facts count as evidence |

### 1.3 Deployment (one AWS VM, D86)

One EC2 instance, Ubuntu 24.04, **`r6a.large` (2 vCPU, 16 GB) in `ap-south-1` with a 4 GB swap file**, a 50 GB unencrypted gp3 volume, accepted for test data only (D135). Switch runs from its **standalone Docker Compose**, pinned to `0.31.0`, with the `collab` (Mattermost) and `gateway` profiles and restart policies from our compose override, so everything returns after a reboot (D124, D136, `docs/RUNBOOK.md`). Switch Console on a laptop is only a client of this server ("Connect to an existing server") through a port-forward. The whole AWS account is capped at **$50 (D110)**, and this size costs about $16 for 8 days around the clock, so stopping it when idle is optional (the disk is kept, and the reconciler recovers at boot, D56). AWS Budgets alerts at $10 and $25, with a budget action that stops the instance at $50 (D135). **No ingress (D64):** GitHub and Jira are polled (GitHub reads every 10 s, Jira JQL every 30 s with an overlap window, D97), so there is no tunnel, domain, static IP or open port. The security group has no inbound rules; all access is SSH over SSM Session Manager. People reach the dashboard (and `/mcp`) with an SSH port-forward (`ssh -L 8080:localhost:8080 heimdall-vm`). If the team later owns a domain on Cloudflare, a named Cloudflare Tunnel can carry `/webhooks/*` and polling switches off. **Exception, Teams (D79):** Microsoft pushes to Switch, so Switch's Teams listener (port 3978) is published through a named Cloudflare Tunnel on the team's domain, set up by the Teams admin (`docs/TEAMS_SETUP.md`). Only that listener is public; Heimdall stays private. **Optional, Mattermost (D107, D161; the team itself uses the port-forward and does not test this):** to use the Mattermost desktop and phone apps, which need a reachable server address, so Mattermost (port 8065) is published through a named Cloudflare Tunnel on a hostname such as `chat.<domain>`. The tunnel is outbound only, so the security group still has no inbound rules. Mattermost's Site URL and the Switch connection's Public Url are set to that hostname, and Cloudflare Access is **not** placed in front (the mobile and desktop apps cannot complete its browser login); Mattermost's own invite-only sign-up, strong generated passwords and rate limits protect it (`docs/MATTERMOST_ACCESS.md`).

**Metadata and privileges (D86, D90).** IMDSv2 is required with hop limit 1, and nftables blocks `169.254.169.254` for every agent uid and every sandbox container, so nothing on the VM can read the instance role's credentials. `heimdall doctor --host` runs first on the VM and checks sudo, systemd, Docker, nftables, free memory and disk, the metadata setting and outbound reach to GitHub, Jira, OpenRouter and Switch. Secrets live in `/etc/heimdall/secrets` (D89).

| Stack | Containers | Memory |
|---|---|---|
| Switch | server, Postgres, Mattermost (standalone compose, D124) | ~3 GB |
| Heimdall | `heimdall-server`, Postgres 16 | ~1 GB |
| Memory | Graphiti MCP server, FalkorDB; embeddings are hosted (D87), so there is no Ollama | ~1 to 1.5 GB |
| Agents | test-author, resolver, profiler and one reviewer agent per person: each an OpenCode session run by Switch's headless agent controller as its own Linux user under systemd, with its own harness directory (D43, D44, D80, D123). Sessions are per room (D145), so rooms are archived at close to free them; T01 measures memory per session. Needs sudo once (D90) and Node 22.13+ for the controller, which is Switch's own component | ~3 to 5 GB |
| Sandbox | `heimdall-runner` under its own user with a rootless container runtime, short-lived jobs, 1 slot (D91) | 2 GB per slot |

Peak is about 10 to 12 GB without Ollama; confirm with `docker stats` on day one. Nothing of Heimdall listens publicly (only Mattermost and Switch's Teams listener, each through its own tunnel route). The dashboard and `/mcp` are reachable only on the Docker network and through SSH port-forwards. Local mode (D38, D157) needs no ingress at all: GitHub is polled and the tracker is a file. Docker networks: `core`, `memory`, and `sandbox` (no egress). Spot instances are used only for eval runs.

---

## 2. `heimdall-server`: Go service design

### 2.1 Process model

One binary, one process, these goroutine groups:

| Group | Concurrency | Job |
|---|---|---|
| HTTP (`net/http` + chi, 10 s read/write timeouts) | n/a | MCP streamable HTTP, dashboard, SSE; webhooks only in `webhook` mode |
| Pollers | 1 per repo, 1 per Jira project | GitHub every 10 s and Jira every 30 s (D64, D69); cursors in Postgres; each new observation becomes an event through the same handlers webhooks use |
| River queue `events` | 16 | Load, reduce, decide, append events |
| River queue `agents` | 32 (one active task per agent, D80) | Wake agents, ack deadlines, `unavailable` marking |
| River queue `sandbox` | 1 on the `r6a.large` (config) | Docker jobs |
| River queue `integrations` | 8 | GitHub, Jira and Switch writes from the outbox |
| River queue `memory` | 4 | Graphiti episodes |
| River periodic | n/a | Reminders, escalations, chain verification, pack expiry, room sync |
| PG listener | 1 | `LISTEN change_updated` → SSE |
| Switch stream | 1 | The coordinator's SSE stream with `filter=all`, all open change rooms declared on the URL; reconnect on room open/archive; cursor in Postgres; `read_context` reconcile on gap, reconnect or boot. Turns only question-thread replies, `heimdall` commands from mapped humans and Switch's unavailable reply into events (D46, D139, D140). Every agent post is also scanned for secrets and canaries, which raises an alert and changes no state (D141) |
| Reconciler | periodic, 5 min and at boot | Compare open PRs and In Progress tickets with local state; append missing events as `actor: reconcile` (D56) |

Every remote call runs as a River job with an idempotency key, exponential backoff with jitter (max 8 attempts, max 10 min delay) and a dead-letter view in the admin dashboard.

### 2.2 Package layout and dependency rule

```
heimdall/                       one Go module
  cmd/heimdall-server/          wiring only
  cmd/heimdall/                 CLI: setup, admin, doctor, pack, eval, mcp (stdio bridge)
  internal/domain/               pure: types, events, reducer, rules (no I/O, imports nothing internal)
    change/ finding/ evidence/ gate/ slop/ deliberation/ policy/ workflow/
  internal/app/                  use cases (HandleWebhook, SubmitReview, EvaluateGate ...)
  internal/ports/                interfaces: EventStore, Projections, VCS, Tracker, Chat, AgentRuntime, Sandbox, Memory, Blobs, Clock, IDs
  internal/adapters/             postgres, github, jira, switch, graphiti, docker, blobfs, agents/{switchroom,headless}
  internal/transport/            webhook, mcp, web (html/template + htmx, go:embed), sse
  internal/security/             hmac, untrusted wrapping, output filter, injection heuristics, redaction rules
  internal/pack/                 collectors, redactor, distill orchestration, age, signing (CLI side)
  internal/platform/             config, slog, OpenTelemetry, River setup, goose migrations
  skills/                        SKILL.md per role, room instructions, versioned
  evals/                         eval corpus and expected outcomes
  internal/testkit/              TEST-ONLY: doubles, payload recorder, helpers. Never imported by production code (D121)
  cmd/heimdall-testagent/        TEST-ONLY scripted agent. Excluded from release builds and images (D121)
  deploy/                        compose files, systemd units, install.sh, reusable CI workflow
```

The dependency rule: `domain` imports nothing internal; `app` imports `domain` and `ports`; adapters implement ports. A CI check enforces it, which keeps the gate and rules testable with plain values.

Main libraries (pinned on day one): chi, pgx v5, River, goose, go-github, the official MCP Go SDK, the Docker Go SDK, filippo.io/age, gitleaks (as a library), invopop/jsonschema, OpenTelemetry Go, testcontainers-go.

### 2.3 Ports

```go
type EventStore interface {
    // Append is optimistic: ErrVersionConflict if the change moved on.
    Append(ctx context.Context, changeID string, expected int64, evs []domain.Event, fx []domain.Effect) (int64, error)
    Load(ctx context.Context, changeID string) ([]domain.Event, error)
}

type VCS interface {
    PR(ctx context.Context, repo string, n int) (PR, error)
    Files(ctx context.Context, repo string, n int) ([]ChangedFile, error)
    TreeHash(ctx context.Context, repo, sha, dir string) (string, error)
    CommitFiles(ctx context.Context, repo, branch, msg string, files []File) (string, error)
    EnsureBranch(ctx context.Context, repo, from, name string) error
    SetCheck(ctx context.Context, repo, sha string, c Check) error
    Tarball(ctx context.Context, repo, sha string) (io.ReadCloser, error)
}

type AgentRuntime interface {
    Wake(ctx context.Context, a AgentRef, t TaskEnvelope) (WakeResult, error)
    Liveness(ctx context.Context, a AgentRef) (Liveness, error) // Live | Dormant | Offline
}

type Sandbox interface {
    Run(ctx context.Context, j SandboxJob) (SandboxResult, error) // JUnit parsed; logs to blobs
}

type Memory interface {
    AddEpisode(ctx context.Context, ep Episode) error
    Search(ctx context.Context, q MemoryQuery) ([]MemoryItem, error) // group ids set by the server, never by callers
}
```

### 2.4 Consistency model

- **Event-sourced per change.** Versions 1..n, enforced by `UNIQUE(change_id, version)`. Use cases load, reduce, decide and append with an expected version. On conflict they reload and retry, up to 5 times.
- **Per-change serialization.** Mutating jobs carry River unique key `change:<id>`, and transactions take `pg_advisory_xact_lock(hashtext(change_id))`. Different changes run in parallel; one change never races itself.
- **Transactional outbox.** Side effects (GitHub check, room post, Jira write, memory episode) are written as `outbox` rows in the same transaction as the event that causes them. Workers drain the outbox. Nothing external happens without a committed event, and every committed effect eventually happens.
- **Idempotent effects.** Each outbox row has a deterministic key (for example `check:<repo>:<sha>:<gate_version>`). Check runs are updated by external id, comments carry a hidden marker, and room posts are deduplicated by key.
- **Derived gate.** `MERGE_ALLOWED` is never stored as truth. It is recomputed for the head SHA after every event and published only when it changes.

### 2.5 Resilience

| Down | Behaviour |
|---|---|
| GitHub | Outbox retries; check keeps its last state; dashboard banner |
| Jira | Start by hand (`heimdall start KEY` in chat, `/heimdall start KEY` on GitHub); rejection tickets queue |
| Switch | Posts queue; agents cannot be woken, so the gate stays red with `agents_unreachable` |
| Graphiti/FalkorDB | `ask_knowledge` returns `unavailable`; no E3 evidence possible; episodes queue |
| OpenRouter / model provider | VM tasks fail; one retry (OpenRouter can fall back across DeepSeek providers), then `unavailable` (red, never green) |
| Docker / `heimdall-runner` | Jobs queue; gate red with `verification_pending` |
| Heimdall itself | The required check stays "waiting" on GitHub, which blocks merges (fail closed). `/healthz` and a heartbeat banner show it; `docs/RUNBOOK.md` has `make recover` and a logged break-glass that toggles the branch protection rule |

Each adapter has a circuit breaker: 5 consecutive failures open it for 30 s, then one probe. Every call has a deadline (GitHub and Switch 10 s, Graphiti 20 s, sandbox per job).

### 2.6 Configuration

`/etc/heimdall/heimdall.toml` (or the file named by `--config`) holds infrastructure: URLs, `secrets_dir`, worker counts, sandbox limits, and the project map below. `heimdall.yaml` in each repo holds policy and workflow (section 16). Secrets are read only from files under `secrets_dir`, default `/etc/heimdall/secrets` (persistent disk, mode 0400, owner `heimdall`), which containers see as Docker secrets at `/run/secrets` (D89). A reboot never loses them. On a laptop `secrets_dir` is the gitignored `secrets/`, filled from `.env` by `make secrets-local` (D148). Config is validated at boot, and the server refuses to start on any error.

```toml
secrets_dir = "/etc/heimdall/secrets"

[[projects]]                 # D147: one tracker project maps to one repo in the build
tracker_project = "KAN"
repo = "aarush323/PBL_TOS_RISK_ANALYZER"
base_branch = "heimdall-demo"
```

---

## 3. Domain model

### 3.1 Identifiers

Changes use the ticket key (`PAY-142`), or `pr-<repo>-<number>` for a PR with no ticket (D146). Everything else is a prefixed ULID (`fnd_…`, `run_…`, `case_…`, `tsk_…`). People never see a ULID: findings have a short per-change id (`F1`, `F2`) stored in the event, used in chat and commands (D156). Agents are named `<person>-<role>` (people, e.g. `bob-reviewer`) or `<role>` (shared, e.g. `test-author`). Switch agent names must match `^[a-z0-9][a-z0-9._-]*$` (V1), so no slashes (D78).

### 3.2 Event catalog

| Event | Key payload | Source |
|---|---|---|
| `change.triggered` | ticket, repo, assignee, trigger source; Jira acceptance criteria as ordered `{id, text, hash, ref}` values | Jira webhook or fallback |
| `room.opened`, `branch.ensured` | room id; branch, base sha | Adapters |
| `task.assigned` / `task.acked` / `task.timed_out` / `task.failed` | task id, agent, role, deadline, bundle hash, reason | Engine |
| `step.retried` / `step.escalated` | step id, attempt, reasons / attempts, reason | Engine (on_fail, 5.1) |
| `question.asked` / `question.answered` | ids, refs, sender, message id | test-author, room sync |
| `tests.submitted` / `tests.verified` / `tests.rejected` / `tests.committed` | refs, AC map, run ids, reasons, sha, tree hash; `tests.verified` also carries `needs_signoff`, set by the engine from policy (sensitive paths and labels, D131, D33): true sends the change to AWAITING_TEST_SIGNOFF, false to TESTS_READY | Agent, sandbox, GitHub, engine |
| `tests.approved` | approver, tree hash (sensitive domains, D33) | Room or GitHub command |
| `ticket.changed` | changed fields | Jira poller |
| `pr.opened` / `pr.pushed` / `pr.linked` / `pr.closed` | number, head sha, author, ticket, merged | GitHub poller |
| `slop.evaluated` | pass, failing rules | Rule |
| `reviewers.picked` | people, by, method (dashboard, command, default) | Dashboard, command, timer |
| `review.submitted` / `review.unavailable` / `review.waived` | reviewer, finding refs, pack date / reason / approver | Agent, engine, decision |
| `finding.filed` / `finding.verified` / `finding.state_changed` | id, tier, state, by | Agents, sandbox, rules |
| `challenge.filed` | finding, round, evidence | Agents |
| `case.opened` / `verdict.submitted` / `verdict.applied` / `verdict.escalated` | case, verdict, citations, checks | Engine, resolver |
| `decision.recorded` | kind, target, login, reason, source ref | GitHub comment or room command (D47) |
| `approval.recorded` / `approval.invalidated` | login, commit sha | GitHub review, push |
| `ci.reported` | check, sha, conclusion | GitHub `check_run` |
| `gate.evaluated` | ok, reasons, sha, policy version | Rule |
| `change.rejected` / `change.merged` | reason / sha | Engine, GitHub |

Every event carries `id`, `change_id`, `version`, `type`, `actor{kind,id}`, `occurred_at`, `causation_id`, `correlation_id` (the trace id), `payload`, `blob_refs`, `prev_hash` and `hash`.

### 3.3 Change states

```
TRIGGERED → ROOM_OPEN → WRITING_TESTS → VERIFYING_TESTS → (AWAITING_TEST_SIGNOFF, sensitive domains only, D33) → TESTS_READY → BUILDING
BUILDING → PR_OPEN → SLOP_REJECTED (↺ on push) | PICKING_REVIEWERS → IN_REVIEW
IN_REVIEW ⇄ DELIBERATING → ESCALATED → IN_REVIEW
IN_REVIEW ⇄ FIXING → VERIFYING_FIX → IN_REVIEW
IN_REVIEW → READY_FOR_APPROVAL → APPROVED → MERGED
any → REJECTED;  WRITING_TESTS..BUILDING → WRITING_TESTS on ticket.changed;  PR states → PR_OPEN on push
UNLINKED_PR (no ticket, D146) → slop failure, stays red; pr.linked moves the PR to its ticket's change and closes this stream
TRIGGERED..TESTS_READY + pr.opened → the PR attaches; the state keeps its place and the gate says "tests are not ready yet" until BUILDING
```

The reducer is a pure `func(State, Event) (State, error)`. An illegal transition is refused. A table test covers every state × event pair, and a property test checks that replaying any valid stream twice gives the same state.

### 3.4 Invariants (tested, and asserted at runtime)

1. No agent can cause `decision.recorded` or `approval.recorded`.
2. An approval counts only when its commit SHA equals the current head.
3. An E4 finding is never in the blocking set.
4. Every `gate.evaluated{ok:true}` used the pinned test tree hash.
5. The policy is read from the merge base, never from the PR head.
6. A `verdict.applied` references only citations re-verified in the same transaction.
7. Silence and timeouts never move a change closer to merge.

---

## 4. Data

### 4.1 Postgres

```sql
CREATE TABLE event (
  seq BIGSERIAL PRIMARY KEY, id UUID NOT NULL UNIQUE,
  change_id TEXT NOT NULL, version BIGINT NOT NULL, type TEXT NOT NULL,
  actor_kind TEXT NOT NULL, actor_id TEXT NOT NULL,
  payload JSONB NOT NULL, blob_refs TEXT[] NOT NULL DEFAULT '{}',
  causation_id UUID, correlation_id TEXT,
  occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  prev_hash BYTEA, hash BYTEA NOT NULL,
  UNIQUE (change_id, version)
);
CREATE INDEX event_type_time ON event (type, occurred_at);

CREATE TABLE outbox (
  id BIGSERIAL PRIMARY KEY, effect_key TEXT NOT NULL UNIQUE, kind TEXT NOT NULL, target TEXT NOT NULL,
  payload JSONB NOT NULL, event_id UUID NOT NULL REFERENCES event(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(), done_at TIMESTAMPTZ, attempts INT NOT NULL DEFAULT 0, last_error TEXT
);
CREATE INDEX outbox_pending ON outbox (created_at) WHERE done_at IS NULL;

CREATE TABLE webhook_delivery (provider TEXT, delivery_id TEXT, received_at TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (provider, delivery_id));

CREATE TABLE cursor (            -- pollers (D64) and the Switch stream (D46)
  source TEXT PRIMARY KEY,       -- e.g. github:acme/pay-demo:pulls, jira:PAY, switch:coordinator
  value TEXT NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Projections can be rebuilt with `heimdall admin rebuild`: `change`, `task`, `question`, `testset`, `review_request`, `review`, `finding`, `challenge`, `case`, `verdict`, `decision`, `approval`, `ci_status`, `run`, `person`, `agent`, `pack_meta`, `policy_version`, `usage`. Each has `last_event_seq`; projectors are idempotent upserts. The application role can only `INSERT` and `SELECT` on `event`, and triggers reject `UPDATE` and `DELETE`.

### 4.2 Hash chain

`hash = sha256(prev_hash || canonical_json(event_without_hash))`, chained per change (versions 1 to n, the first event has no `prev_hash`) and canonicalized with a documented subset of RFC 8785 (`internal/domain/change/hash.go`: sorted keys, no whitespace, UTC times, integers only, duplicate keys refused). A global chain by `seq` would force every append to wait for the previous one, which the per-change optimistic version (2.4) avoids; deletion of an event is already blocked by the triggers in 4.1. A chain alone cannot show that the tail of a change was cut off, so the nightly job verifies every change's chain and publishes the signed head hash of each to an admin repo file and the room log (decided Nov 4, T11).

### 4.3 Blobs

Stored at `/var/lib/heimdall/blobs/sha256/<2>/<2>/<hash>`, written atomically (temp file plus rename), with a metadata row for size, type, retention class and refcount. Packs are stored as `age` ciphertext only. Retention: logs and diffs 90 days; anything referenced by a decision, verdict or approval is kept; packs are deleted on revocation.

### 4.4 Backups

Nightly `pg_dump` and blob rsync off-box, plus a nightly FalkorDB RDB snapshot. `heimdall admin restore --verify` replays and checks the chain.

---

## 5. Workflow engine

### 5.1 Workflow shape (a Go value in the build, D54; the YAML loader is Later)

```yaml
workflow:
  version: 1
  steps:
    - { id: open_room,   on: "jira.transitioned{to:'In Progress',assignee:human}", do: switch.open_room, with: { invite: [role:test-author, person:$assignee] } }
    - { id: branch,      after: open_room,   do: github.ensure_branch, with: { name: "{ticket}-{slug}" } }
    - { id: write_tests, after: branch,      do: agent.task, with: { role: test-author, output: TestSubmission@1, deadline: 2h } }
    - { id: test_qa,     after: write_tests, do: sandbox.test_quality, with: { checks: [red_first, ac_coverage, flake, contracts] },
        on_fail: { retry: write_tests, until: task_budget, then: escalate } }
    - { id: commit,      after: [test_qa, signoff], do: github.commit_tests, with: { dir: "tests/heimdall/{ticket}" } }
    - { id: wait_pr,     after: commit,      do: wait, with: { event: pr.opened } }
    - { id: slop,        after: wait_pr,     do: rule.slop, on_fail: reject }
    - { id: pick,        after: slop,        do: human.pick_reviewers, with: { default: suggest, timeout: 15m } }
    - { id: review,      after: pick,        do: agent.fanout, with: { role: reviewer, from: review_requests, output: Review@1, deadline: 30m } }
    - { id: verify,      after: review,      do: sandbox.verify_evidence }
    - { id: signoff,     after: test_qa,     do: human.approve_tests, when: sensitive_domain }   # D33
    - { id: mutation,    after: review,      do: sandbox.mutation, with: { scope: changed_lines } }   # Later
    - { id: deliberate,  after: verify,      do: deliberation.run, with: { resolver: role:resolver, max_rounds: 2 } }
    - { id: gate,        on: any,            do: rule.merge_allowed }
```

The same shape becomes YAML later. Whether built in Go or loaded from YAML, the workflow is validated at start:
- the graph is a DAG apart from declared retries;
- every `after` and every role exists;
- every output schema is registered;
- every timeout is bounded.

An invalid workflow fails closed with `policy_invalid`. Adding a stage, such as a performance reviewer, is one step plus a skill.

### 5.2 Step contract

```go
type Step interface {
    Type() string
    Start(ctx context.Context, c ChangeView, with json.RawMessage) (Result, error)
    OnEvent(ctx context.Context, c ChangeView, e domain.Event) (Result, error)
    OnTimeout(ctx context.Context, c ChangeView) (Result, error)
}
type Result struct { Events []domain.Event; Effects []domain.Effect; Timers []Timer }
```

Steps do no I/O themselves. They return events, effects and timers; the engine persists them in one transaction, and the outbox and River dispatch them. That makes every run replayable and every step unit-testable.

---

## 6. Agent layer

### 6.1 Roles

| Role | Runs | Default model (D73) | Purpose |
|---|---|---|---|
| test-author | VM, Switch `always_on`, OpenCode runtime | `deepseek/deepseek-v4-flash` | Ticket → questions if needed → failure-first tests |
| reviewer | VM (Switch remote host), `always_on`, one container and harness per person (D43, D44), OpenCode runtime | `deepseek/deepseek-v4-flash` | Review through that person's approved pack |
| resolver | VM, OpenCode runtime | `deepseek/deepseek-v4-flash` (policy can name a stronger OpenRouter model) | Settle disputes using evidence and memory |
| author's agent | developer's laptop | owner's choice | Build, fix, claim fixes |
| coordinator | inside `heimdall-server`, not an LLM | n/a | Switch identity of the harness |

Each LLM role is a skill plus the `heimdall` MCP tools. **VM agents run on OpenCode with OpenRouter (D72)**: Switch supports OpenCode agents (V18), OpenCode talks to OpenRouter natively, and Claude Code is not used on the VM because it only officially supports Anthropic models. No Anthropic API is used anywhere. Under OpenCode the model id carries the provider, `openrouter/deepseek/deepseek-v4-flash`, because Switch splits it at the first slash (D143). The skills stay portable to Claude Code and Codex for developers' own laptops.

### 6.2 Task lifecycle

1. `task.assigned` (deadline, budget, bundle hash) → the outbox posts an addressed room message: `@bob-reviewer you have a new task. Call heimdall_get_task.` The task id is not posted, because the agent's token finds it (D156). An agent has at most one active task; others queue (D80).
2. The agent calls `heimdall_get_task` (its token is bound to agent and role) and receives the envelope.
3. `heimdall_ack` within 60 s. Otherwise `task.timed_out` follows and the request is marked `unavailable` (gate red, waivable by an approver, D43). Switch's own "unavailable" auto-reply (V16) does the same at once. Switch restarts a crashed agent itself; Heimdall holds no gateway login and restarts nothing (D145).
4. The agent submits with its role's tool. Schema and semantic validation run (6.5). On failure the agent gets structured errors and keeps working inside the task's time and cost budget; running out of budget means `task.failed` (D122).
5. A late submission is accepted but marked late. The first valid submission wins; later ones are attached as supplementary.
6. Sessions are per room, so no context crosses from one change to another. A re-review in the same room keeps its context (D145).

### 6.3 Task envelope

For Jira work, `acceptance_criteria` contains the ordered criterion IDs and refs captured by `change.triggered`. The
trigger stores each source text as a content-addressed `BlobEvidence` object and records its SHA-256 and ref in the
event, so a replay produces the same input. Criterion text remains untrusted: it is not in the role bundle or the task
envelope and is available only through `heimdall_read_untrusted`, which wraps the text as data rather than instructions.

```json
{
  "task_id": "tsk_01J...", "role": "reviewer", "skill": "reviewer@1.0.0",
  "change": { "id": "PAY-142", "repo": "acme/pay-demo", "base_sha": "a01b...", "head_sha": "9f1c..." },
  "goal": "Review this change from your owner's perspective. File only findings you can back with evidence.",
  "inputs": {
    "diff": "blob:sha256:...",
    "files_changed": ["internal/refunds/refunds.go", "api/openapi.yaml"],
    "acceptance_criteria": [{ "id": "AC2", "ref": "blob:sha256:..." }],
    "ci": [{ "name": "heimdall/acceptance", "conclusion": "success" }],
    "untrusted": [{ "source": "pr_body", "ref": "blob:sha256:..." }, { "source": "jira_description", "ref": "blob:sha256:..." }]
  },
  "context_budget_tokens": 150000, "output_schema": "Review@1", "deadline": "2026-11-05T14:00:00Z"
}
```

### 6.4 Context engineering

`heimdall_get_task` returns a server-built, deterministic **context bundle** per role, stable parts first so they cache, under a cap (test-author 40k, reviewer 60k, resolver 30k tokens). Tools are for drilling down, not discovery. The bundle is stored as a blob and its hash is recorded on `task.assigned`, so evals replay exactly what the agent saw. Bundle contents per role are in D45. Entity extraction for memory queries is plain code (paths, routes, identifiers from `oasdiff` and `go/ast`). Each agent's working directory, tool allowlist and egress are fixed by the per-role harness (D44): no repo checkout, no GitHub credentials, code only through `read_code` at the SHA the role allows.

What each role may pull beyond its bundle, in priority order:

| Role (total budget from policy `budgets`) | 1st | 2nd | 3rd | 4th | Never |
|---|---|---|---|---|---|
| test-author (400k incl. retries) | AC and answers | Contracts (OpenAPI, interfaces), related existing tests | Memory: decisions, incidents, invariants for touched entities | Base source files | Feature-branch non-test changes |
| reviewer (150k) | Diff and changed files at head | Own pack: ownership, invariants, incidents for touched paths | Memory on touched entities | CI and static results | Other people's packs |
| resolver (120k) | Case file | Memory: prior decisions and verdicts, with validity times | AC | Code around the disputed lines | Raw chat |

Rules every skill states:
- Untrusted content is read only through `read_untrusted`.
- Cite every claim (`file:line`, run id, memory ref, pack ref).
- No evidence means `reasoning` (shown as unverified).
- No diff restating; no style nits unless policy enables them.
- Compact at 70% of the budget, keeping citations.

### 6.5 Output discipline (server-side, beyond JSON Schema)

- Every `location.file` must exist in the head tree, and its line range must exist.
- Every cited run id, memory ref and pack ref must exist and belong to this change or caller.
- **Deduplication:** findings from different reviewers that overlap in file and lines and have similar claims (normalized token Jaccard ≥ 0.6) merge into one finding with several reporters.
- A finding outside the diff needs category `compat | ops | security` and a reason.
- **Anti-sycophancy:** a reviewer cannot withdraw a finding without citing changed evidence. "The author explained" is not evidence.
- Free text through `report_progress`: 1 per 2 min per task, at most 400 chars, through the output filter. Agents also hold Switch's room tools, which cannot be denied (D141). Skills tell them to report only through `heimdall_*`, the coordinator scans their posts for secrets and canaries, and the board fails a run where a reviewer posts a finding early.

### 6.6 Cost control

- **Cost control (D128).** One production OpenRouter key with a credit limit is the hard cap. Tasks are bounded by a deadline per role and by the context bundle caps, not by a per-task dollar enforcer. Spend is read from the key's usage and shown on the dashboard. Separate keys exist only for testing (`TEST_STRATEGY.md`).
- The VM runtimes cache stable prefixes (skill, room instructions, pack).
- The slop rule runs before any agent, so rejected PRs cost zero tokens.
- Reviewers are capped at the default 3 plus any policy requires.
- Re-review on push wakes only reviewers whose files changed.
- Graphiti extraction uses DeepSeek V4 Flash through OpenRouter, and ingestion is selective.

### 6.7 Agent quality: evals

| Suite | Cases | Metrics | Ship bar |
|---|---|---|---|
| reviewer | 20 diffs with known bugs (some catchable only with a pack or memory fact), 10 clean diffs | recall, precision, evidence rate, false-block rate | recall ≥ 0.6, precision ≥ 0.7, evidence ≥ 0.8 |
| test-author | 10 tickets with reference and planted-wrong implementations | red-first rate, AC coverage, wrong-impl kill rate, mutation score | red-first ≥ 0.9, kill ≥ 0.8 |
| resolver | 15 disputes with known outcomes | accuracy, correct `needs_human` rate | accuracy ≥ 0.8, zero wrong auto-applies |
| injection | 12 payloads across every untrusted source | state changes | 0 |

Full-corpus evals are not affordable at the $3 testing budget, so the build runs the reduced corpora in `TEST_STRATEGY.md` section 4 (D60, D113); the ship bars above apply afterwards. `heimdall eval run --suite reviewer --skill reviewer@1.1.0` runs headless in sandboxes. Skill versions are pinned in policy, and a new version ships only if it beats the old one on its suite. Results show in the admin dashboard.

### 6.8 Agent adapters

| Adapter | Use | Mechanism |
|---|---|---|
| `switchroom` | default | Addressed `post_message`; liveness from participants and agent detail; detects the unavailable reply |
| `headless` (later) | ephemeral reviews, evals | `opencode run` (or another runtime) in a fresh container, one task, destroyed |

### 6.9 Agent runtime and supervision (D80, D123, D125)

How the VM agents actually run. Switch ships a **headless agent controller** for exactly this, and we use it instead of building our own supervisor.

| Concern | Rule |
|---|---|
| Runtime | OpenCode 1.x, pinned in `RUNBOOK.md` by T01 (Switch provider `opencode`, D144), model `openrouter/deepseek/deepseek-v4-flash` (D143), with the model's own settings (no forced temperature, D122). The definition sets `auto_mode: true`, `auto_approve: false` (D141) |
| Process | `switch-agent-controller`, enrolled with a one-time code from the gateway's Machines page and installed with `install-service --separate-users`. That creates a pool of system users (`sa<uid>-01` and so on, 16 by default), one systemd template unit per agent, a polkit rule that lets the controller start, stop, restart and reset those units and nothing else, and agent directories under `/var/lib/switch-agents/<uid>`. It runs as a system service, so it survives a reboot. Requires the server flag `FEATURE_FLAGS_ENABLED=agent_management`, Node 22.13+ (Switch's own component), the `opencode` CLI on the path, and the OpenRouter key and `SWITCH_CONTROLLER_SELF_UPDATE=off` in the controller's `--env-file` (D123, D144) |
| Switch credentials | None in the agents. The controller holds one connection and agents reach Switch through its local relay with a token only the relay accepts |
| Working directory | Per agent, under `<agents-dir>/agent/<agent-name>/`: `AGENTS.md` (role rules), `opencode.json` as in the example below (mode 0600, holding that agent's token, D142), `.opencode/skills/<role>/SKILL.md`, `scratch/`. No pack files (reviewers read their own pack through `heimdall_read_pack`, D156), no repo checkout, no GitHub or Jira credentials |
| Network | nftables keyed on the agents' group (`meta skgid`) allow only the Switch relay, `heimdall-server` and `openrouter.ai`, and drop `169.254.169.254` (D86). This holds even if an OpenCode permission is wrong |
| Concurrency | One active task per agent; further tasks queue in River (unique key `agent:<name>`) |
| Isolation between changes | Sessions are per room and each change has its own room, so nothing crosses between changes; rooms are archived at close (D145, proven by T01) |
| Liveness | Ack within 60 s, else `unavailable` (gate red, waivable). Switch restarts crashed agents (D145) |
| Output | Only through `heimdall_*` tools; schema and semantic checks; on failure the agent gets the errors and keeps working within its budget (6.2, 6.5, D122) |
| Cost | One production key with a credit limit (D128) |
| Profiler | A fourth LLM role (OpenCode, same harness rules) that reads repo files through `read_code` and drafts the repo profile and CI proposal (D98, D99) |

**Harness `opencode.json` (D125, D141, D142).**

```json
{
  "$schema": "https://opencode.ai/config.json",
  "autoupdate": false,
  "share": "disabled",
  "permission": {
    "*": "deny",
    "bash": "deny",
    "webfetch": "deny",
    "websearch": "deny",
    "doom_loop": "deny",
    "external_directory": { "*": "deny" },
    "edit": { "*": "deny", "**/scratch/**": "allow" },
    "heimdall_*": "allow"
  },
  "mcp": {
    "heimdall": {
      "type": "remote",
      "url": "http://127.0.0.1:8080/mcp",
      "headers": { "Authorization": "Bearer __HEIMDALL_AGENT_TOKEN__" }
    }
  }
}
```

Agents run on the host under systemd, so they reach the server's loopback port, not a Docker name. `admin init` replaces the placeholder per agent. Rules match in order and the last match wins, so the catch-all comes first. **Switch adds its own session rules after this file:** in `auto` mode `read`, `glob`, `grep`, `list`, `lsp`, `todowrite`, `question`, `skill`, `task` and the `switch` MCP tools are always allowed, so this file does not limit them (V59). Reads outside the working directory are stopped by `external_directory` and the separate Linux users. T03 checks all of this on the real VM.

---

## 7. W1: ticket to failing tests

```mermaid
sequenceDiagram
  participant J as Jira
  participant P as heimdall-server
  participant R as Room (Mattermost)
  participant T as test-author
  participant M as Memory
  participant D as Developer
  participant S as Sandbox
  participant G as GitHub
  P->>J: JQL poll sees PAY-142 → In Progress
  P->>R: open room, Jira + repo refs, invite test-author and the developer
  P->>G: ensure branch PAY-142-refunds
  P->>T: task
  T->>P: get_task, read_untrusted(jira), read_code(base), ask_knowledge
  P->>M: search (groups = teams ∪ shared)
  opt real gap
    T->>P: ask_humans (≤3)
    P->>R: question card @dev
    D->>R: answers (linked account)
  end
  T->>P: submit_tests (+ expected_paths)
  P->>S: red_first, ac_coverage, flake, seam imports
  opt sensitive domain (D33)
    P->>R: test sign-off card @domain approver
  end
  alt fail
    P->>T: structured reasons (retry within the task budget)
  else pass
    P->>G: commit tests/heimdall/PAY-142/* (as App), pin tree hash
    P->>R: "6 failing tests ready"
  end
```

Rules:
- The server commits the tests; the test-author has no write credentials.
- The test-author reads only the base branch.
- At most 3 questions. If unanswered after `timeouts.question` (4 h), it proceeds on stated assumptions, written into the test header and the room.
- `ticket.changed` on description or AC re-runs W1 as a delta and re-pins.
- Test sign-off follows explicit rules (D33, D131). If the ticket has the label `needs-test-signoff`, or its AC or the test-author's `expected_paths` match the policy's `tests.sensitive` list, a domain approver must approve the tests (`heimdall approve-tests` in chat, `/heimdall approve-tests` on GitHub) before the developer is told to build. Everywhere else no sign-off is needed. The developer can always challenge a test (section 11).

## 8. W2: PR to merge

```mermaid
sequenceDiagram
  participant G as GitHub
  participant P as heimdall-server
  participant S as Sandbox
  participant R as Room
  participant A as Author
  participant RV as Reviewers (one per person, on the VM)
  participant RS as Resolver
  participant AP as Approver
  P->>G: poll sees new PR (head X)
  P->>P: slop rule
  alt fail
    P->>G: gate = failure (rule named)
    P->>R: reason; Jira ticket if policy
  else pass
    P->>G: gate = in_progress
    P->>R: @author pick reviewers (default in 15m)
    A->>P: pick (/heimdall review on the PR, heimdall review in the room)
    P->>RV: tasks (ack 60s, else unavailable)
    par
      RV->>P: reviews + findings
    and
      P->>S: static, contract
    end
    P->>S: verify evidence → tiers
    P->>R: summary, blocking set, cards
    opt dispute
      P->>RS: case → verdict
      P->>R: decision card with the resolver's recommendation (D42)
    end
    P->>G: poll sees push Y → approval invalidated, re-verify, re-wake reviewers
    AP->>G: APPROVED on Y
    P->>G: poll sees approval; gate = success iff merge rule holds on Y
  end
```

### 8.1 Reviewer selection

- **Who picks:** the PR author, with `/heimdall review @a @b` on the PR or `heimdall review @a @b` in the room from their mapped account. The dashboard picker is Later (D53).
- **What is picked:** people. Each resolves to that person's reviewer agent on the VM, which runs with their approved pack (D43). Laptops are used only at setup, to build the pack.
- **Default:** `reviewers.count = 3`, configurable per repo and path. If nobody picks before `pick_timeout`, the default applies: CODEOWNERS of the changed paths, then top recent committers to them, excluding the author.
- **Required reviewers from policy** are added and locked. Only policy can exceed the count.
- **Unavailable reviewers:** if the agent does not ack in time, the request is marked `unavailable` and the gate stays red until an approver waives it (a recorded decision).

---

## 9. Team memory: Graphiti + FalkorDB (D28)

### 9.1 Why

- Facts carry `valid_at`, `invalid_at` and `created_at`, and new facts invalidate contradicting ones. That is our observed/confirmed/superseded lifecycle for free, plus "was this true at commit time".
- Hybrid search covers keyword, embedding and graph distance.
- `group_id` partitions give team scoping, which the server enforces.
- There is an official MCP server, it is Apache 2.0, and FalkorDB keeps it light. We write no memory engine.

### 9.2 Ontology (custom entity types steer extraction)

| Entities | Examples |
|---|---|
| Person, Team | bob, payments |
| Service, Module, API, Field | refunds service, `internal/refunds`, `POST /refunds`, `refund_ref` |
| Decision, Invariant | "Refunds idempotent by Idempotency-Key", "Refund ≤ original payment" |
| Incident, Ticket, PR | INC-031, PAY-142, #88 |

Edges: `DECIDED`, `APPLIES_TO`, `CAUSED_BY`, `DEPENDS_ON`, `OWNS`, `SUPERSEDES`, `FIXED_BY`.

### 9.3 Ingestion

| Source | Trigger | Episode | group_id | Status |
|---|---|---|---|---|
| Human decision, applied verdict | `decision.recorded`, `verdict.applied` | structured JSON (claim, scope, who, when, links) | owning team + shared | confirmed |
| Merged change summary | `change.merged` | DeepSeek V4 Flash summary: what changed, AC, findings resolved | owning team | observed |
| Resolved findings | `finding.state_changed → resolved/upheld` | claim and resolution | owning team | observed |
| Jira ticket | `change.triggered`, `ticket.changed` | summary, AC, links | project team | observed |
| Incident | Jira type Incident, or the `incident` command | description, cause, fix | owning team + shared | observed (confirmed when a postmortem is accepted) |
| Change room digest (D65) | change merged or rejected | one digest of the room from Switch history (`read_context`, V35): Q&A, decisions and commands, findings summary, outcome | owning team | decisions confirmed, rest observed |
| Team channels (D65) | rooms in `memory.rooms`, every 30 min; only threads with policy keywords or `heimdall remember` | thread | room team | observed |
| ADRs and docs | push to the default branch | document | repo team + shared | confirmed if ADR is accepted |
| Backfill | `heimdall admin backfill` | 12 months of merged PRs with reviews, incidents, ADRs | as above | as above |

The server tracks status for each episode (Graphiti stores the fact; the server stores which source produced it). Episodes go through the outbox with key `episode:<source>:<id>:<version>`. Personal packs are never ingested.

### 9.4 Query and scoping

Agents never reach Graphiti. `heimdall_ask_knowledge(question, entities?, as_of?)` works like this:
1. Resolve the caller → person → teams → `group_ids = teams ∪ {shared}`. The server sets these; callers cannot.
2. Run a Graphiti fact and node search with those groups, limit 20.
3. Label each item with the D51 rules: `confirmed` (at least one confirmed source episode, not validly invalidated as of `as_of`, default the head commit time), `superseded` (invalidated by a confirmed episode), `contested` (a confirmed fact an observed episode contradicts; still confirmed, shown with the contradicting source), otherwise `observed`.
4. Return the items with source URL, `valid_at`, `invalid_at`, label and a stable `memory_ref`.
5. Log the query and the result refs on the task (audit and eval data).

Only confirmed, non-superseded items are E3 evidence.

### 9.5 Cost, performance, failure

- DeepSeek V4 Flash extraction, selective sources, live episodes sent in order, backfill batched.
- Hosted embeddings (D87, V50): `mongodb/voyage-4-lite` through OpenRouter, 1024 dimensions at $0.02 per million tokens. Graphiti MCP config (D66, verified V37, V38): `llm.provider: openai` with `api_url: https://openrouter.ai/api/v1` and model `deepseek/deepseek-v4-flash` (D73; Graphiti needs JSON output, so S13 checks the chosen variant honours `response_format`), `embedder.provider: openai`, `embedder.providers.openai.api_url: https://openrouter.ai/api/v1`, `embedder.model: mongodb/voyage-4-lite`, `embedder.dimensions: 1024`, `database.provider: falkordb`. The Graphiti MCP server has no auth, so it stays on the internal `memory` network. Heimdall's `Memory` adapter is a client of the Graphiti MCP server and calls `search_memory_facts` and `search_nodes` with `group_ids` and the temporal filters (`valid_at_after`, `invalid_at_before` and the like), all of which the server's tools accept (D126, read in its source).
- Query cache of 60 s per (groups, normalized question).
- If Graphiti is down: `unavailable`, no E3, episodes stay queued, dashboard alert.
- **Alternative behind the same port:** Cognee, if Graphiti disappoints.

---

## 10. Context packs

### 10.1 Contents

```
pack/
  manifest.json      person, version, created_at, source summary, scanner versions, file hashes, signature
  ownership.md       owned or well-known paths, with evidence (commit share, blame share)
  invariants.md      rules this person enforces, with provenance
  incidents.md       incidents and bugs fixed or seen
  conventions.md     conventions they hold
  review_history.md  what they flag often
  unknowns.md        areas they do not know (agent defers)
  sources.md         knowledge sources (names, URLs; never credentials)
```

Every rule or fact line has provenance (`[git:3f2a1c]`, `[pr:#88]`, `[transcript:2026-09-12]`, `[doc:docs/adr/004.md]`). Findings cite pack line ids (`pack:bob:invariants#L12`), never content.

### 10.2 Collection (laptop only; raw data never leaves)

| Source | Location | Kept |
|---|---|---|
| Claude Code | `~/.claude/projects/<encoded-path>/*.jsonl` (deleted after `cleanupPeriodDays`, default 30; setup offers to raise it, D68) | user and assistant text from sessions with cwd in the repo; tool outputs dropped |
| Codex | `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl` (format varies by version) | same |
| OpenCode | `~/.local/share/opencode/storage/{session,message,part}/` JSON (secondary source, V45) | same; skipped with a warning if not found |
| Git | `git log --author`, sampled `git blame` | ownership, touched areas, commit messages |
| Review history | GitHub API with the person's own token | their review comments |
| Markdown | CLAUDE.md, AGENTS.md, READMEs, `docs/**`, ADRs, a named notes folder | text |
| Knowledge sources | MCP config names, bookmarks | names and URLs only |

### 10.3 Pipeline

1. **Collect** into a 0700 temp directory.
2. **Redact pass 1, before any model sees anything:**
   - the gitleaks rule set (embedded as a Go library) plus our own patterns: cloud keys, JWTs, private keys, connection strings, `.env` lines, bearer headers;
   - a high-entropy detector;
   - PII: emails, phones, Luhn-checked card numbers, government ids;
   - internal hostnames and the team deny-list.

   Matches become typed placeholders. Found keys are never verified live.
3. **Distill** with the developer's own local agent and the pack skill: patterns, not passages; no quote over 200 chars; provenance kept.
4. **Redact pass 2** on the pack. Any hit blocks upload.
5. **Owner review** (mandatory): the full pack plus a summary of what was removed. The developer edits and approves.
6. **Sign** the manifest (ed25519, the person's key).
7. **Encrypt** with `age` to the VM recipient.
8. **Upload** to blobs. The server keeps only metadata in Postgres.
9. **Serve:** `heimdall_read_pack` decrypts in the server, verifies hashes and returns only the calling reviewer's own pack, refusing packs older than `pack.max_age_days` (14). Ciphertext stays at rest. A stale pack gives findings that are unverified (D156).

`heimdall pack refresh` rebuilds incrementally. Revocation deletes blobs, stops the person's reviewer agent and revokes tokens.

---

## 11. Evidence, deliberation, resolver

### 11.1 Tiers

| Tier | Forms | Harness check |
|---|---|---|
| E1 | `repro_test` | Run on head in the sandbox; JUnit `<failure>` required; `<error>` gives E4 |
| E2 | `tool_result`: `oasdiff`, `go vet`, `staticcheck`, `semgrep`, mutation survivors (per-language list in policy) | Tool re-run; location must match |
| E3 | `memory_ref` confirmed and not superseded, or a prior applied verdict or decision | Re-queried at verification |
| E4 | `reasoning`, `pack_ref`, observed memory | None; shown as unverified |

The blocking set is findings in `open | disputed | escalated | upheld | fix_claimed | verifying` that are either at tier E1 to E3 with severity ≥ `blocking.min_severity`, or **consensus-blocking** (11.2b). Any other high or critical E4 finding opens a human card without blocking.

### 11.2 Finding states

```
proposed → open (E1–E3) | unverified (E4)
open → disputed → open | withdrawn | escalated
escalated → upheld | dismissed | spec_changed     (resolver per 11.4, or human)
open | upheld → fix_claimed → verifying → resolved | open
```

Only the filing reviewer can withdraw, and only with changed evidence. Only a runner re-run or the filing reviewer's re-verification resolves. The author's agent can only claim fixes.

**Every push re-verifies (D152).** No claim is needed: E1 and E2 evidence for open findings is re-run on the new head and a pass resolves the finding. E3 and E4 findings on changed files go back to the filing reviewer. A human author challenges with `heimdall challenge F3 <evidence>`; `claim_fix` for laptop agents is Later (D160).

### 11.2b Consensus scoring (D34, amended by D40)

Hard evidence is preferred, but strong agreement between independent reviewers can hold a merge for a human.

**Build rule (D40):** a merged finding with at least `consensus.min_high_reporters` (default 2) distinct reviewers at `high` confidence becomes `consensus_hold`: gate red with reason `consensus_hold`, a human card opens, and only a human decision or new E1 to E3 evidence clears it. The weighted score below is **(Later)**: it applies once a reviewer has `weight.min_decided` decided findings. (With the original defaults, two `high` reviewers at the starting weight 0.7 score 1.4, below 1.5, so the weighted rule could never fire on a new team; G5.)

The weighted rule, for later:

- **Independence:** reviewers run in parallel, `get_change` hides other reviewers' findings until every assigned reviewer has submitted or timed out, and the server posts only fixed-template progress to the room during the review phase (D41).
- **Confidence:** every finding carries the reporter's confidence: `high` = 1.0, `medium` = 0.5, `low` = 0.2.
- **Merging:** findings on the same issue merge (overlapping file and lines, claim similarity ≥ 0.6), keeping each reporter and their confidence.
- **Calibration weight per reviewer:** `w = clamp(accepted / (accepted + rejected), 0.3, 1.0)`, starting at 0.7 with fewer than 5 decided findings. A finding counts as accepted when it is upheld or resolved by a fix, and rejected when it is dismissed or withdrawn.
- **Score:** `S = Σ confidence × w` over reporters.
- **Consensus-blocking** if `reporters ≥ ceil(assigned/2)` and `S ≥ consensus.block_score` (default 1.5). The gate stays red with reason `consensus_block`, and a human card opens showing reporters, confidences, weights and score. Only a human decision, or new E1 to E3 evidence that resolves it, clears it. Never the resolver alone.
- **Split views** (some reviewers flag, others submit clean on the same files) are recorded as a disagreement with counts and scores and attached to the resolver's case file. They are not a vote.
- All inputs are stored, so the dashboard shows exactly why something blocked.

### 11.3 Settlement order

1. **Re-run** E1 or E2 evidence. The result decides; no model involved.
2. **Challenge rounds.** Each round must add new verifiable evidence; max 2.
3. **Resolver.** It receives the case file: finding, positions, every run, AC and policy. It must call `ask_knowledge` for prior decisions, incidents and verdicts on the same entities. It returns a verdict (`uphold | dismiss | amend | spec_change | needs_human`), a rationale, citations and a confidence.
4. **Human** decision card.

### 11.4 Auto-applying a verdict (Later; in the build every verdict is a recommendation on a card, D42)

All of these must hold:
- At least one E1 to E3 citation, or a confirmed decision that settles the point under the precedence order: confirmed decision > executable result > deterministic tool > owner statement > observed memory > inference.
- The server re-verifies every citation in the same transaction.
- The domain is not in `human_only_domains` (default `auth`, `money_movement`, `data_deletion`), and severity is below critical.
- Confidence is `high`.

Otherwise the verdict becomes a recommendation on a card. Every verdict is an event and a memory episode. An approver can reverse an applied verdict with `/heimdall override <case> <reason>`.

### 11.5 Human decisions

Decisions arrive from strict-grammar GitHub comments by a verified actor, or (D47) the same grammar posted in the change room by a human whose Switch sender id maps to `people.<p>.chat` (D140). Final merge approval is always a GitHub PR review. The dashboard decision inbox is Later (D53).

```
/heimdall review @a @b @c                          author
/heimdall uphold <finding>                         approver or domain owner  (<finding> is the short id, F3)
/heimdall dismiss <finding> <reason ≥15 chars>     filing reviewer's person or approver
/heimdall spec-change <text>                       approver
/heimdall override <case> <reason>                 approver
/heimdall waive-reviewer @x <reason>               approver
/heimdall reject <reason>                          approver
/heimdall approve-tests                            domain approver (sensitive domains, D33)
/heimdall incident <text>                          member (records an incident episode)
/heimdall start <TICKET>                           member (start by hand)
/heimdall challenge <finding> <evidence>           author (test path at head, run id, memory ref or decision ref, D152)
/heimdall remember <text>                          member (records an observed fact, chat form of !remember in D65)
/heimdall forget <memory_ref> <reason>             approver (invalidate a poisoned fact)
```

That is the GitHub form. **In chat the same grammar starts with the plain word `heimdall`** (`heimdall uphold F3`), with no `!`, `/` or `@`, because Switch and the platforms take posts starting with `!` or `/` as their own commands and never pass them on (D139, V57). An unrecognised `heimdall …` post gets one short reply with the grammar. Edited comments and other text are ignored and never sent to a model. Without D47, chat replies count only for answering questions and picking reviewers (V7).

---

## 12. Tests, sandbox, slop, gate

### 12.1 Test quality (D4)

| Check | When | Pass |
|---|---|---|
| Red-first | submission | every test fails on base with JUnit `<failure>`, zero `<error>` |
| AC coverage | submission | every AC id tagged by at least one test (`// heimdall:AC2`) |
| Flake | submission and PR | 3 runs; unstable tests quarantined and excluded from the gate |
| Seam (contracts) | submission | tests import only the profile's `seam`, the standard library and `tests.allowed_imports` (the Go profile checks this with `go list -deps -test`; other profiles declare their own import rule under `evidence`, D98), and exercise behaviour over HTTP. A missing route gives an assertion failure; a compile error is an `<error>` and rejects the submission with compiler output (D39) |
| Green | PR | `heimdall/acceptance` CI job passes on head |
| Mutation (Later) | PR | Go: `go-mutesting --git-diff-lines` or a wrapper around Gremlins, which has no diff mode (V47); Stryker (TS) or mutmut (Python) on changed lines, heimdall tests only. The score is shown and tracked; below 0.6 the change gets `tests_too_weak` and the test-author is re-woken to strengthen tests. It does not block (`tests.mutation_blocks: false`) |

### 12.2 Sandbox

| Job | Command (Go example) | Limits |
|---|---|---|
| `red_first` | the profile's `acceptance` with `{dir}` = `tests/heimdall/<KEY>` (Go: `gotestsum --junitfile /out/junit.xml -- ./{dir}/...`) on base + tests | 2 CPU, 2 GB, 180 s |
| `repro` | same on head + finding test | 2 CPU, 2 GB, 120 s |
| `static` | `go vet`, `staticcheck`, `semgrep --config <pinned>` on changed files | 2 CPU, 2 GB, 180 s |
| `contract` | `oasdiff breaking base.yaml head.yaml -f json` | 1 CPU, 512 MB, 30 s |
| `mutation` (Later) | `go-mutesting --git-diff-lines` on changed lines (V47) | 2 CPU, 2 GB, 600 s |

Isolation:
- Images come from an allowlist in `heimdall.toml` and are pinned by digest (Go, Python, Node, JVM and a generic base), one container per job. A profile cannot name any other image (D98).
- **Untrusted code never runs in `heimdall-server`.** The server hands jobs to `heimdall-runner` over a Unix socket (`run(profile, tarball sha)` returns a JUnit summary and a log blob). The runner has its own user, a rootless container runtime and none of Heimdall's secrets, and **the server never holds the Docker socket**. Containers run rootless with the default seccomp profile (D91); stronger isolation such as gVisor is a later hardening, not a conditional path. Jobs cannot reach the metadata address (D86).
- Dependencies come from a read-only module cache volume, warmed by a separate fetch job. That is the only job with egress, allowlisted to the package registries the profile needs (Go proxy, npm, PyPI, Maven Central, crates.io) plus any `fetch_hosts` in the merged profile (D153).
- Execution runs with `--network none`, or for a job with services a private `--internal` network shared only with them (D153), with a read-only root, tmpfs `/work` and `/out`, `--cap-drop ALL`, `no-new-privileges`, the default seccomp profile, a 512 PID limit, a non-root user, no secrets and no Docker socket.
- Source is a tarball the server fetches at the exact SHA.
- Output is the parsed JUnit summary plus logs capped at 5 MB in blobs. Agents get summaries; raw logs only via `read_untrusted`.

### 12.2b Repo profile (D98, D99)

The sandbox is generic. Each repo has `.heimdall/profile.yaml`:

```yaml
languages: [python, javascript]
image: python:3.12-slim@sha256:...        # must be on the allowlist
setup: pip install -r backend/requirements.txt
test: pytest backend/tests --junitxml=/out/junit.xml        # the repo's own tests
acceptance: pytest {dir} --import-mode=importlib --junitxml=/out/junit.xml   # Heimdall's tests; {dir} is set per job (D153)
seam: backend.main:app                     # exists on base; tests reach the app only through it
ac_tag: "# heimdall:{ac}"                  # comment form for this language
fetch_hosts: [github.com]                  # beyond the registries, fetch job only; a human approves by merging
services:                                  # throwaway containers, private network, no egress
  - { name: db, image: "pgvector/pgvector:pg16@sha256:...", env: { POSTGRES_PASSWORD: test-only } }   # Jurist AI's migrations need pgvector
evidence:                                  # per-profile tools; the Go profile defaults to go vet, staticcheck, oasdiff, go list -deps
  - { id: compose, run: "docker compose config -q", tier: E2 }   # needs an allowlisted image with the compose CLI; no daemon is used
paths: { tests: tests/heimdall }
```

Flow: deterministic detectors (marker files such as `go.mod`, `pyproject.toml`, `package.json`, `pom.xml`, `Cargo.toml`, `Dockerfile`, `docker-compose.yml`) build a draft; the **profiler** agent fills the gaps through `read_code` at a SHA; a **PR** proposes the profile and `heimdall.yaml`, and for a repo without CI its body holds the exact `.github/workflows/heimdall-acceptance.yml` for a person to commit (D161); a human merges it; Heimdall validates it (image on the allowlist, no network at execution, limits, and a trial run that produces JUnit) before the sandbox uses it. CD is out of scope, and an agent never merges its own profile.

### 12.3 Slop rule (pure)

Inputs are GitHub API data plus policy. A PR passes only if:
- the ticket key appears in the title, branch or body;
- changed lines minus ignored paths and minus `tests.dir` are ≤ `max_changed_lines` (D49);
- tests changed, or heimdall tests exist for the ticket;
- changed paths outside `tests.dir` are within the test-author's `expected_paths`, or the PR body has a non-empty justification section, which is never interpreted.

### 12.4 Merge rule (pure)

```
ok ⇔ ticket linked
   ∧ slop.passed(head)
   ∧ testset.verified ∧ testset.ac_covered ∧ (¬sensitive(testset) ∨ testset.approved)
   ∧ tree_hash(head, tests.dir/KEY) = testset.tree_hash
   ∧ diff(merge_base, head, tests.dir) ⊆ tests.dir/KEY
   ∧ ci(head, heimdall/acceptance) = success ∧ ∀c ∈ ci_required: ci(head, c) = success
   ∧ ∀r ∈ review_requests: covered(r, head) ∨ waived(r)
   ∧ blocking_set = ∅ ∧ no case awaiting a human
   ∧ ∃ approval: APPROVED ∧ commit = head ∧ login ≠ author ∧ login ∈ approvers(domains(changed paths))
   ∧ (policy(merge_base) = policy(head) ∨ policy_owner_approved(head))
```

`heimdall/acceptance` runs all of `tests.dir`, so earlier tickets' tests guard against regressions, while each pin covers only its own ticket (D150). `covered(r, head)` means r's last review was on a SHA whose diff to head touches none of the files r reviewed. The check summary lists every unmet clause in plain language, and a table test flips each clause.

---

## 13. Integrations

**GitHub App.**
- Permissions (five, D161): checks write; contents write, limited in code to `tests/heimdall/**` on ticket branches and to memory and policy PRs; pull requests read/write; issues read/write; metadata read. The App never writes workflow files. An installation may hold more; `doctor` notes extras, and narrowed tokens and `[[projects]]` limit what Heimdall touches.
- Events: `pull_request`, `pull_request_review`, `issue_comment`, `check_run`, `check_suite`, `push` (default branch), `create`.
- Rate limits: an ETag read cache keyed by SHA and a token bucket per installation.
- **Narrowed tokens (D96, V51):** two token sources. `reader` (contents, pull requests, issues, checks, all read) serves polling and bundles. `writer(op, repo)` is minted on demand with only that operation's permission and only that repository (`checks:write` for check runs, `contents:write` for the tests branch, `pull_requests:write` and `issues:write` for comments) and cached for at most 50 minutes. The App needs only the five agreed permissions, and `heimdall doctor` reads the installation's real permissions from the API (V54).
- Repo setup by `admin init`. Branch protection cannot go through a PR and the App holds no `administration`, so `admin init` prints one `gh api` command for a repo admin: required checks `heimdall/gate` and `heimdall/acceptance` with admin bypass off and "dismiss stale approvals on new commits" on. `doctor` then checks the result (T04 confirms what it can read). A PR carries CODEOWNERS on `tests/heimdall/**`, `heimdall.yaml` and `.github/workflows/**` naming the human approvers team (Apps cannot be code owners; the tree-hash pin is the primary control, D49); the reusable `heimdall-acceptance.yml`.
- `github.mode: poll | webhook`, default `poll` (D64). Poll reads per repo every 10 s: open PRs, issue comments since the cursor, and reviews and check runs only for PRs whose head SHA or `updated_at` changed; about 1,000 to 2,000 requests per hour against at least 5,000 (D69). ETags help little because installation tokens rotate hourly. On 403/429 the adapter honours `Retry-After`/`x-ratelimit-reset` and doubles its interval.

**Tracker.** Port with two adapters: Jira Cloud and `file` (`seed/tickets/<KEY>.md`). AC are the bullets under an "Acceptance criteria" heading; the server assigns `AC1..n` and tracks each by a hash of its text (D50).

**Jira.** A JQL poll every 30 s per `[[projects]]` entry (`project = <tracker_project> AND status changed to "In Progress" after <cursor>`) with a human assignee triggers a change; webhook mode is optional. Reads use REST v3 with an API token (a bot user or scoped token is Later). **Each poll re-queries from `cursor - 10 min` and deduplicates by (issue key, changelog id)**, because JQL resolves to the minute and the search index can lag (V55, S20); the 5-minute reconciler stays. Descriptions arrive as Atlassian Document Format (V52), so a small walker (`internal/adapters/jira/adf`) yields text and the bullets under a heading named "Acceptance criteria", with golden tests on real KAN tickets (D97). Writes (rejection tickets, comments) are gated by policy. A JQL backfill feeds memory.

**Switch.**
- The coordinator registers once (V1).
- Operations used: `create_room`, `invite_agent_to_room`, `add_users_to_room`, `create_reference`, `attach_reference_to_room`, `post_message`, `read_context`, `create_room_document`, `update_room_document`, `update_room`, `archive_room` (V2).
- All VM agents and the coordinator share one owner (the admin), and agents use template `addressing: owner-agents`, so the coordinator can wake them (V34). Humans link their Mattermost or Teams accounts (V7). The coordinator also uses `list_participants` to fill the people map.
- No Switch tasks (V4).

**Chat.**
- **Minimal chat port (D127).** `Chat` has three operations: create or adopt a room, post, read. Every command is a text command starting with the plain word (`heimdall uphold F3`), identical on every platform (D139). `/heimdall` is the GitHub comment form. Final merge approval is always a GitHub review on the head commit. The only platform-specific behaviour is the Teams quirk below, selected by `chat.bridge`; there is no capability negotiation. Mattermost is tested by us (Teams if it lands); Slack, Discord and Telegram work through Switch and are documented as untested.
- **People mapping (D95).** `people:` in `heimdall.yaml` maps the Switch sender id (`chat`, opaque and matched whole, D140), GitHub login and Jira account id. Commands from unmapped senders are ignored, which refuses agents by default; a person who is unmapped is told how to link. A decision needs a mapped human and an allowed role. A merge approval is a GitHub review on the head SHA.
- One room per change.
- Milestones posted: opened, tests ready, PR received, reviewers picked, findings summary, escalation, approved, merged or rejected.
- @-mentions only for questions and decisions.
- `verbosity: milestones | detailed`, plus per-person quiet hours.
- `!agents-status` is documented for teams (V16). `!run-cmd` is not used: Switch's changelog says the command it printed did not bring an agent online, and its reply no longer carries a start command.
- **Teams (D79, D81):** rooms are created on the bridge named in `chat.bridge`. In Teams, agent messages render as cards and **@-mentions from agents are plain text**, so humans are not notified by them; the coordinator therefore also posts every question and decision card as a reply in the channel thread the person follows, and the dashboard lists open asks. Commands work as `heimdall …`.

---

## 14. Interfaces

### 14.1 MCP tools (`/mcp` streamable HTTP; `heimdall mcp` is a stdio bridge)

| Tool | Roles |
|---|---|
| `heimdall_get_task`, `heimdall_ack`, `heimdall_get_change` | all |
| `heimdall_read_untrusted(ref)` | all |
| `heimdall_read_code(path, sha, range?)` | all (test-author: base SHA only) |
| `heimdall_read_pack(section?)` | reviewer (own pack only, D156) |
| `heimdall_ask_knowledge(question, entities?, as_of?)` | all |
| `heimdall_run_check(kind, args)` | reviewer, resolver (counts toward budget) |
| `heimdall_submit_tests` | test-author |
| `heimdall_ask_humans(questions ≤3)` | test-author, resolver |
| `heimdall_submit_review` | reviewer |
| `heimdall_challenge(finding, evidence)` | reviewer, author |
| `heimdall_claim_fix(finding, sha, evidence)` (Later, D160) | author |
| `heimdall_submit_verdict` | resolver |
| `heimdall_report_progress(text)` | all |

Schemas are generated from Go structs and versioned (`Review@1`). The previous version stays accepted for one release.

```go
type FindingIn struct {
    Claim       string     `json:"claim"`        // 10..600 chars
    Category    string     `json:"category"`     // correctness|security|compat|ops|spec_drift|tests|style
    Severity    string     `json:"severity"`     // low|medium|high|critical
    Confidence  string     `json:"confidence"`   // high|medium|low (feeds consensus scoring)
    Location    Location   `json:"location"`     // File, Lines [2]int
    Evidence    []Evidence `json:"evidence"`     // ≥1
    ContextRefs []string   `json:"context_refs"` // ≤8 pack or memory refs
}
type Evidence struct { // tagged union, exactly one set
    Type       string      `json:"type"` // repro_test|tool_result|memory|pack_ref|reasoning
    ReproTest  *ReproTest  `json:"repro_test,omitempty"`
    ToolResult *ToolResult `json:"tool_result,omitempty"`
    MemoryRef  *string     `json:"memory_ref,omitempty"`
    PackRef    *string     `json:"pack_ref,omitempty"`
    Reasoning  *string     `json:"reasoning,omitempty"`
}
type Verdict struct {
    CaseID     string     `json:"case_id"`
    Decision   string     `json:"decision"`   // uphold|dismiss|amend|spec_change|needs_human
    Rationale  string     `json:"rationale"`
    Citations  []Citation `json:"citations"`  // run|memory|decision|finding
    Confidence string     `json:"confidence"` // high|medium|low
}
```

### 14.2 HTTP

| Route | Auth | Purpose |
|---|---|---|
| `POST /webhooks/github`, `/webhooks/jira` (Later, D160) | HMAC | ingest |
| `/mcp` | agent bearer | agent tools |
| `/`, `/changes/:id` | none in the build: reachable only through the SSH port-forward (D53, D64) | read-only dashboard (htmx partials) |
| `/inbox`, `/admin/*` (Later) | GitHub OAuth session + CSRF | decision inbox, admin |
| `/sse/changes/:id` | same as the dashboard | live updates |
| `/healthz`, `/readyz` | none | probes (`readyz` checks Postgres, River, Docker, Graphiti) |
| `/metrics` | internal network | Prometheus |

### 14.3 Dashboard

| View | Who | Content |
|---|---|---|
| My changes | developer | stage, tests (AC map, quality), reviewers (working / submitted / unavailable), findings by tier with evidence, cases, verdicts, blockers in plain words |
| Change timeline | repo members | event log, runs and logs, room, PR and ticket links |
| Reviewer picker | PR author | people search, suggestion, count, locked required reviewers |
| Decision inbox | approvers, owners | cards: uphold, dismiss, spec-change, override (reason required) |
| Admin | admin | gate pass rate, findings by tier, dismissals and overrides, verdict auto-apply vs escalation, agent liveness, queue depth and dead letters, memory lag, tokens by change/agent/role, pack ages, eval results |
| Policy | admin, policy owners | validated editor; saving opens a PR |
| People and agents | admin | GitHub ↔ Switch ↔ Mattermost links, pack metadata, revoke |

---

## 15. Security

### 15.1 Threat model

Assets, in order of harm:
1. merge integrity;
2. source and secrets;
3. personal context;
4. team memory;
5. the audit trail.

Attackers: an outside PR author, a careless or malicious insider, poisoned content (tickets, chat, docs, dependencies, test output), and a compromised agent host.

### 15.2 Prompt injection (layered)

| Layer | Control |
|---|---|
| Separation | Untrusted text is never placed in instructions; it is fetched only via `read_untrusted`, wrapped with a random 128-bit boundary and a fixed preface (data, not instructions; report embedded instructions as an `injection` finding) |
| Least authority | No agent tool approves, merges, dismisses, resolves, edits policy or tests, or pushes (except the author's agent to its own branch). The worst an injected agent can do is submit a wrong claim |
| Verification | Only harness-verified E1–E3 evidence blocks; verdicts apply only under 11.4; free text never changes state |
| Output filter | Secret scan, pack-overlap cut, link-to-text rewrite and @-mention stripping on every MCP submission and progress message. Agents' direct room posts cannot be filtered, so they are scanned afterwards and raise an alert (D141) |
| Detection | Heuristics (instruction phrases, role markers, hidden Unicode, zero-width characters, HTML comments) flag inputs and show `injection_suspected`; used for visibility, not relied on for safety |
| Memory hygiene | Only decisions, verdicts and accepted ADRs are confirmed; chat and PR text stay observed; episodes keep their source; `/heimdall forget` invalidates poisoned facts |
| Regression | The injection eval suite runs in CI; any state change fails the build |

### 15.3 Developers' context

- Raw transcripts and notes never leave the laptop.
- Two redaction passes; any hit blocks upload; owner review is mandatory.
- `age` encryption at rest. The server decrypts only to answer `heimdall_read_pack` for the owner's reviewer agent, after the signature is checked (D156).
- One container and Unix user per reviewer agent, no shared volumes, egress allowlist (Switch, server, `openrouter.ai`, GitHub read).
- Canary strings planted in each pack; the output filter and an egress log scan alert on any appearance.
- Packs never enter team memory, and findings cite pack line ids, not content.
- Revocation deletes blobs, stops the reviewer agent and rotates tokens.

### 15.4 Platform

| Area | Control |
|---|---|
| Webhooks | HMAC-SHA256 over the raw body, constant-time compare, delivery-id dedup, 5-min window, 1 MB cap, 30 req/s per source |
| Agent tokens | 32 random bytes, stored as SHA-256, bound to agent, role and person, valid only for assigned tasks, rotated on pack refresh, revocable |
| Ingress | None for Heimdall (D64): GitHub and Jira are polled; only Mattermost (people's apps, D107) and Teams (D79) are published through Cloudflare Tunnel; dashboard and `/mcp` via SSH port-forward and the Docker network only; the AWS security group has no inbound rules and access is SSH over SSM (D135); IMDSv2 with hop limit 1, and the metadata address is blocked for agents and sandbox containers (D86) |
| Dashboard | Build: read-only, no login, private (SSH forward only). Later: GitHub OAuth with PKCE, roles from policy, `HttpOnly`/`Secure`/`SameSite=Lax` cookies, CSRF tokens, CSP `default-src 'self'` |
| GitHub writes | Path and branch allowlist in the adapter, plus a per-operation token narrowed to one repository and one permission (D96) |
| Untrusted code | Runs only in `heimdall-runner` in rootless containers; the server never holds the Docker socket (D91) |
| Secrets | `secrets_dir`, mode 0400, persistent (D89; the hackathon VM's disk is not encrypted, so it holds test credentials only, D135); Docker secrets inside containers; never in logs (a slog redaction handler), rooms, packs or blobs; gitleaks in CI |
| Supply chain | `go.sum`, images pinned by digest, `govulncheck` in CI, SBOM, cosign-signed release binaries |
| Audit | Hash-chained events, nightly verification; decisions record identity, SHA and reason |
| Abuse and cost | Slop first, reviewer cap, budgets, per-agent rate limits, sandbox semaphore |

### 15.5 Permission matrix

| Actor | Read code | Write branch | Write tests | Memory read | Memory write | Run code | Gate check | Jira | Approve/dismiss |
|---|---|---|---|---|---|---|---|---|---|
| test-author | base (tool) | no | via server | scoped | no | via server | no | no | no |
| reviewer (per person, VM) | base + head (tool) | no | no | scoped | no | via server | no | no | no |
| author's agent | yes | own branch | no | scoped | no | own machine | no | no | no |
| resolver | case + code (tool) | no | no | scoped | no | via server | no | no | no |
| server | exact-SHA tarballs | test commits only | yes | yes | episodes | yes | yes | yes | no |
| humans | per GitHub | per GitHub | via decision | dashboard | `heimdall remember`, decisions | n/a | no | yes | per policy role |

---

## 16. Policy file

```yaml
version: 1
workflow: default
approvers: { default: [alice], auth: [ana], money_movement: [alice, bob] }
policy_owners: [ana]
people:
  # chat is the opaque Switch sender id, filled by `heimdall connect people` (D140); names here are placeholders
  alice: { github: alice, chat: alice, teams: [payments] }
  bob:   { github: bob,   chat: bob,   agent: bob-reviewer, teams: [payments] }
  dana:  { github: dana,  chat: dana,  teams: [payments] }          # the PR author in the demo; github is required only for authors and approvers (D158)
  ana:   { github: ana,   chat: ana,   agent: ana-reviewer, teams: [security] }
  eli:   { github: eli,   chat: eli,   agent: eli-reviewer, teams: [consumer] }
domains: { "internal/auth/**": auth, "internal/refunds/**": money_movement }
reviewers: { count: 3, pick_timeout: 15m, required: { "internal/auth/**": [ana], "api/openapi.yaml": [eli] } }
slop: { max_changed_lines: 400, require_ticket: true, ignore: ["go.sum"] }
tests:
  dir: tests/heimdall
  allowed_imports: [github.com/stretchr/testify]   # seam and the acceptance command live in .heimdall/profile.yaml (D153)
  sensitive: ["backend/**/auth*", "**/migrations/**", "*delete*", "*privacy*", "*consent*"]   # D131, the Jurist AI default
  min_mutation_score: 0.6
  mutation_blocks: false
blocking: { min_severity: medium, min_tier: E3 }
consensus: { min_high_reporters: 2, block_score: 1.5, min_reporters: majority, confidence: { high: 1.0, medium: 0.5, low: 0.2 }, weight: { start: 0.7, min: 0.3, max: 1.0, min_decided: 5 } }
deliberation: { max_rounds: 2, human_only_domains: [auth, money_movement, data_deletion] }
keys: { openrouter: production }   # D128: one production key with a credit limit
models: { provider: openrouter, test_author: openrouter/deepseek/deepseek-v4-flash, reviewer: openrouter/deepseek/deepseek-v4-flash, resolver: openrouter/deepseek/deepseek-v4-flash, extraction: deepseek/deepseek-v4-flash, embedding: "openrouter:mongodb/voyage-4-lite" }
budgets: { test_author: 400000, reviewer: 150000, resolver: 120000, per_change: 1200000 }
skills: { test_author: "1.0.0", reviewer: "1.0.0", resolver: "1.0.0" }
memory: { rooms: [payments-eng], chat_keywords: [decided, decision, "we will", incident, postmortem] }
pack: { max_age_days: 14 }
timeouts: { question: 4h, remind_after: 2h, escalate_after: 8h, reject_after: 24h }
ci_required: [build, lint]
chat: { bridge: mattermost, verbosity: milestones, decisions_in_room: true }   # bridge: teams when the Teams connection exists (D79)
github: { mode: poll, interval: 10s }                      # D64
tracker: { kind: jira, mode: poll, interval: 30s }         # kind: file in local mode
agents: { runtime: opencode }   # D72, D145
```

The file is validated against its schema at load and read from the merge base. An invalid policy fails closed.

---

## 17. Setup and operations

**Install.** `curl -fsSL https://<host>/install.sh | sh` installs the signed `heimdall` binary with a checksum check. The setup skill works in Claude Code, Codex and OpenCode, drives the CLI, and stops on any failure with the fix.

**`heimdall admin init`** (once; idempotent and resumable, each step recorded):
1. `heimdall doctor --host`, then VM compose, secrets in `/etc/heimdall/secrets`, the security group (no public ports beyond what the install needs), IMDS hop limit 1, deploy Switch at its pinned version with generated secrets and the restart override (D124, D136, D137), start the core and memory stacks, and print how to connect Console.
2. GitHub App from a manifest, then a PR with CODEOWNERS, then the printed workflow file and branch protection command for a repo admin, which `doctor` checks (D151, D161).
3. Jira token and the JQL poll (webhook optional).
4. Coordinator and role agents in Switch (template, provider `opencode`, one owner), `addressing: owner-agents` (V34), and the headless agent controller enrolled with `--separate-users`, so each agent runs as its own Linux user under systemd with its harness directory (D123, D80).
5. Graphiti entity types, groups and backfill.
6. `heimdall.yaml` via PR.

**`heimdall setup`** (once per developer, target < 15 min):
1. Detect the developer's own coding agent (for distilling the pack) and install the setup skill.
2. Link their GitHub login (device flow) and check their Mattermost or Teams account is linked in Switch.
3. Build, review and upload the pack; the server starts or refreshes their reviewer agent on the VM.
4. `heimdall doctor`.

**Plug-and-play integrations and `heimdall connect` (admin, once per install).** Every external system sits behind a port (`VCS`, `Tracker`, `Chat`, `AgentRuntime`, `Memory`). Adapters register themselves in a small registry (`internal/adapters/registry.go`: `name`, `kind`, `required config`, `Check()`), and `heimdall.toml` selects them by name: `vcs.provider = github`, `tracker.provider = jira | file`, `chat.bridge = mattermost | teams | slack | discord | telegram`. Adding a platform means one adapter package plus one registry line; the domain and app layers do not change. Chat platforms are Switch's job, so `chat.bridge` only names a Switch connection and the Teams-specific behaviour under Operations (D79, D81: thread replies, plain-text mentions) stays in one `chatquirks` table keyed by platform.
- `heimdall connect` lists every registered integration with its status (`connected`, `needs config`, `error`) and what each needs.
- `heimdall connect <kind>` runs an interactive picker for that kind, prompts for non-secret values, tells the admin which secret file to create under `/etc/heimdall/secrets` (it never accepts a secret in an argument), writes the choice to `heimdall.toml`, and runs the adapter's `Check()` (a read-only call).
- `heimdall connect --list`, `--check` and `--json` are non-interactive for scripts. `heimdall doctor` runs the same checks.
- `heimdall connect mattermost` also handles the public address (D109): it asks for it or accepts "none, internal only", checks that the name resolves over HTTPS and answers as a Mattermost server (its ping endpoint, confirmed in S22), and prints the exact settings to change (Site URL, the Switch connection's Public Url) with a link to `docs/MATTERMOST_ACCESS.md`. Heimdall never owns or buys a domain; in a customer deployment the address is on the customer's own domain.
- **`heimdall connect <kind>` guides the setup of every integration, not only one (D137, T99).** Each adapter's registry entry declares its guided steps as data: what the person does in the other app, which secrets it needs, what the CLI does through an API, and the check that proves it worked. The flow is the same for all: show the next manual step, wait, read the secret file, apply the setting, run the live check, and resume where it stopped. Only steps that need a person in the other app stay manual. If an API is missing for a step, the CLI prints the exact Console action instead, then checks the result. Adding an integration means adding its steps to the registry.
- Owner-approved sequence (Nov 5, 2026): core workflow connections first—Mattermost/Switch, Jira, GitHub, OpenRouter and agent controller. After the full core workflow passes (T110), plan Telegram, Slack, Discord and Teams in a separate session (T111) and implement only the connections in that approved plan (T112). This supersedes the older platform-by-platform build order here; it does not remove any integration from scope.

| Integration | The person does | The CLI does | Live check |
|---|---|---|---|
| Switch and Console | Signs in to Console | Deploys the pinned stack (`admin init`), starts or prints the port-forward | Health endpoints; server reachable through the forward |
| GitHub | Approves the App from a manifest and installs it | Stores the key path, opens the setup PR | Installation readable; repos listed |
| Jira | Creates an API token | Stores site, email and project | `myself` and the project both answer |
| OpenRouter | Creates a key with a credit limit | Stores it | Key endpoint shows the limit and balance |
| Mattermost | Provides a domain if the apps need one | Uses Switch's bundled server; sets Site URL, public address and invite-only sign-up | Ping endpoint over HTTPS; bridge active |
| Telegram | Creates the bot, turns Group Privacy off, adds it to a group and posts | Creates the Switch connection; finds the new room | Bridge active; room and message arrived |
| Slack | Creates the app from Switch's manifest and installs it | Prints the manifest, checks both tokens, creates the connection | Bridge active; room and message arrived |
| Discord | Creates the bot, turns on intents, invites it, copies the server id | Prints the invite settings, creates the connection | Bridge active; room and message arrived |
| Teams | Completes Azure and Microsoft 365 admin steps, provides a public hostname | Prints each value needed, publishes port `3978` to the tunnel, creates the connection | Public endpoint answers; bridge active; message arrived |
| Agent controller | Copies the one-time code from the Machines page, if no API can issue it | Installs and enrols the controller with `--separate-users` | Machine online; test agent answers |

**Operations.**
- `heimdall admin upgrade`: signed images, goose migrations (forward-only; down paths tested for the last release), 30 s graceful drain.
- Backups as in 4.4.
- Telemetry: OpenTelemetry traces (root span per change; child spans per step, task and external call), Prometheus metrics, slog JSON logs with the trace id.
- Key metrics: queue depth and age, outbox lag, webhook latency, gate latency, sandbox wait, agent ack latency, task success rate, tokens per change, memory lag.

---

## 18. Performance design

- The webhook path only verifies, persists and enqueues. No external calls.
- The gate is computed from projections: one SQL query plus GitHub data cached by SHA, no model calls.
- File lists and trees are cached by SHA, so each head SHA is fetched once (ETags help little with rotating App tokens, D69).
- Fan-out is bounded by the reviewer cap, the `agents` queue concurrency and the sandbox semaphore.
- Postgres: indexes on `(change_id, version)` and projections by `change_id`, a partial index on open findings, a pgx pool of 20, and `LISTEN/NOTIFY` instead of UI polling.
- Memory queries are cached for 60 s; episodes are async and never on the gate path.
- Load test with k6 against fake GitHub and Switch: 50 concurrent changes, 10 webhooks/s sustained, checked against 0.3.

---

## 19. Testing

The binding strategy is `docs/TEST_STRATEGY.md` (D111 to D120): layers, the per-task loop, the definition of done, the cost budget and a matrix of red test, real check and LLM cost for every task. In summary:

| Level | Scope | Tools | Cost |
|---|---|---|---|
| L0 Static | vet, staticcheck, lint, dependency rule, gitleaks | `make fast` | $0 |
| L1 Unit, property, fuzz | Reducer (every state x event; replay property), rules (each merge clause flipped), tiering, slop, policy validation, redaction patterns; fuzz for ADF, JUnit, command grammar and the redactor | `go test`, table tests, `testing/quick`, Go fuzzing | $0 |
| L2 Component | Adapters on real Postgres (testcontainers-go), the real `heimdall-runner` and sandbox on hermetic fixture repos, the real HTTP server | `make verify` | $0 |
| L3 Contract | Parsers and adapters against **recorded real payloads** (GitHub, Jira ADF, Switch, Mattermost, JUnit); MCP schemas; golden files. `make drift` compares them with the live systems | golden files | $0 |
| L4 Real integration, no LLM | Real GitHub App, Jira KAN, Switch and Mattermost with a **scripted agent** (D118): full W1 and W2 plus replayed events, self-approval, stale-SHA approval, test deletion, E4-only block attempt, challenge without new evidence, stale reviewer pack, and each dependency down | `make e2e` | $0 |
| L5 Live LLM smoke | One real OpenCode agent per role on a tiny fixture task; structural assertions only | `make e2e` | about $0.03 per run |
| L6 Agent evals | 6.7, reduced corpora and a priced budget; the injection suite against live agents (D115) | `make eval` | within the $3 plan |
| L7 Rehearsal and soak | The Jurist AI scenario; the planted-issue check (D122); a 2-hour soak with scripted agents | `make rehearse`, `make soak` | about $0.15 per run; soak $0 |
| Chaos-lite | Kill the server mid-step, kill the runner, stop Postgres, pause Switch, flood the sandbox queue; assert no lost events and no false green (D117). The k6 load test is out of scope | scripted | $0 |

CI runs L0 to L3 on every push. Server-side injection defenses are in L1 and L2, so they run on every push at no cost. Live-agent evals never run on every push; they run on demand, at G3 and G4, and before a skill bump, each after a budget check (D113, D115).

---

## 20. Seed repo `pay-demo` (Go)

**Status (D116).** `pay-demo` and `ledger-demo` are now **hermetic fixtures** (`testdata/repos/`), used for cheap, repeatable tests of the generic sandbox and the evals. The live demo target is Jurist AI (D101), and its scenario (a ticket, planted issues, and the frontend API client as the cross-team fact) is authored as task T75. The PAY-142 story below is the template for it.

- **Service:** `POST /payments`, `POST /refunds`, `GET /refunds/{id}`, with an in-memory store and `api/openapi.yaml`.
- **Consumer:** a separate public repo `ledger-demo` parses `refund_ref`; `pay-demo` has no copy of it, so only eli's pack and memory know (D57).
- **Memory backfill:** ADR "refunds idempotent" (accepted), INC-031 (`refund_ref` removal broke the ledger), and past PRs with reviews.
- **Branches:**
  - `good-pr`: three findings that need three people's context: partial refunds over the payment total (bob), `refund_ref` renamed (eli), any merchant can read any refund (ana). Exact content in D57.
  - `bad-pr`: 1,400 lines, no ticket.
  - `injection-pr`: payloads in the body, a comment and the code.
- **Published as:** `aarush323/heimdall-fixture-pay`, pushed from `testdata/repos/pay-demo` by `make fixtures-push`, so real PRs and checks work (D149). Live tickets get their keys from Jira at run time; `PAY-142` is the file-tracker example.
- **Seam:** `app.New(app.Config) http.Handler` exists on base (D39).
- **Tickets:** `seed/tickets/PAY-142.md` mirrors the Jira ticket for local mode.
- **Baselines:** CodeRabbit and a plain DeepSeek V4 Flash reviewer (same model as ours, so the difference is context, not model) on the same diff, saved verbatim in `seed/baseline/` (D58).

---

## 21. Delivery plan

**History only.** `MVP_SCENARIOS.md` section 7 is the one build order, and `IMPLEMENTATION_PLAN.md` holds the tasks (D160).

| WP | Package | Done when |
|---|---|---|
| 1 | Switch smoke: coordinator opens a room and wakes an agent in Mattermost | the agent replies |
| 2 | Addressing, linking, `doctor` | a fresh agent wakes from the coordinator; doctor is green |
| 3 | Server skeleton: config, Postgres, goose, event store and chain, reducer, River, outbox, HMAC | forged webhook rejected; replay ignored; chain verifies; `kill -9` mid-job loses nothing |
| 4 | Step registry and the Go-defined default workflow (D54) | the default workflow runs on fakes; an invalid one fails closed |
| 5 | GitHub App adapter (checks, tree hash, commit, branch, tarball, ETag cache) | pending `heimdall/gate` on a `pay-demo` PR |
| 6 | Jira poller, file tracker and fallbacks | In Progress (or a file ticket) opens the room and branch |
| 7 | Sandbox: images, module cache, JUnit parser, red-first, flake, repro | import-error test rejected, assertion-red accepted, flaky quarantined |
| 8 | test-author skill and tools | tests for every AC land on the branch, verified |
| 9 | Pack pipeline and reviewer-agent start | a planted secret never reaches the pack; each reviewer agent decrypts only its own pack |
| 10 | Reviewer skill and harness (D44), pick by command or chat, fan-out to remote-host reviewers (D43), context bundles (D45) | the author picks 3; all three review from the VM with only their own pack; the bundle hash is on each task |
| 11 | Evidence verification and blocking set | a repro finding is E1 only when the runner confirms |
| 12 | Graphiti, FalkorDB, embedder, ontology, episodes, scoped `ask_knowledge`, backfill | a consumer finding cites a confirmed decision with `valid_at` and URL; an observed chat fact cannot block |
| 13 | Deliberation, resolver (recommendation only, D42), consensus hold (D40), cards, command grammar in GitHub and room (D47) | the dispute reaches the resolver; its verdict appears on the card; a human upholds from the room; two high-confidence reviewers on the same E4 issue hold the gate pending a human |
| 14 | Merge rule and check | each clause has a flip test; a real PR goes red → fixed → approved on head → green |
| 15 | Read-only dashboard with SSE (inbox and admin Later) | live updates through a full run |
| 16 | Mutation (Later) | a weak test set reports `tests_too_weak` |
| 17 | Security suite: injection evals, output filter, canaries, sandbox hardening | zero injected state changes; the canary alert fires in the test |
| 18 | Eval corpus and runner | v1 skill baselines recorded |
| 19 | Install, admin init, local mode (D38), README, video | a clean machine reaches a red-then-green change in local mode in under 20 min, with no Jira and no public URL |

- **Critical path:** 1, 2, 3, 5, 7, 8, 10, 11, 14.
- **Cut order if time runs short:** 16; the auto-apply part of 13 (keep the resolver as a recommendation); the admin view; chat memory sync; the full eval corpus in 18 (keep the injection suite).
- **Freeze before parallel work:** the event catalog (3.2), the MCP schemas (14.1), the task envelope (6.3), the policy schema (16) and the merge clauses (12.4).

---

## 22. Risks

| Risk | Mitigation |
|---|---|
| Agents do not wake | WP1–2 first; unavailable-reply detection; one container restart; `doctor` |
| Graphiti extraction cost or quality | DeepSeek V4 Flash, selective episodes, custom ontology, confirmed-only evidence; Cognee behind the same port |
| Graphiti config | Verified in source (V37): `openai` embedder provider with `api_url` and `dimensions: 1024`; hosted `voyage-4-lite` returns 1024 (V50); S13 confirms end to end |
| DeepSeek V4 Flash tool-calling or instruction-following is weak | Evidence is verified by code, so a weak model costs recall, not safety; the planted-issue check (2 of 3) before recording; policy can point one role at a stronger OpenRouter model |
| Switch or OpenCode does not behave as read in the source (commands, sessions, permissions, version) | T01 to T03 run first (`STAGE_1_FIXES.md`); the Unix-user and nftables rules hold even if a permission rule is wrong (D80) |
| Teams ingress or tenant consent stalls | Teams is set up by a teammate in parallel; Mattermost stays the default bridge, so the demo never depends on Teams |
| Weak or crashing generated tests | Red-first (failure vs error), AC coverage, flake, mutation, retries |
| Resolver overreach | Auto-apply only with re-verified evidence, never in human-only domains, always reversible |
| Context leakage | Two scans, owner review, encryption, output filter, canaries |
| Sandbox escape or exhaustion | No network, dropped capabilities, seccomp, limits, non-root, semaphore |
| Scope for four people | Critical path, frozen interfaces, pure domain package, cut order |
| Untrusted PR code on the VM that holds secrets | `heimdall-runner` under its own user, rootless runtime, no Docker socket for the server, metadata blocked (D86, D91) |
| Limited login, no sudo | `doctor --host` fails clearly; sudo is a requirement the friend provides (D90) |
| Out-of-memory kill | 16 GB with swap, hosted embeddings, one sandbox slot, memory guard, builds and unit tests on a laptop where possible (D87, D102, D110) |
| AWS spend passes the $50 cap | `r6a.large` (D135), no NAT gateway or load balancer, Budgets alerts and a stop action, a local cost estimate in `doctor` (D110) |
| Mattermost tunnel down or misconfigured (people cannot connect) | Switch and the agents keep working inside the VM; S22 tests desktop and phone apps through the tunnel before the demo; `docs/MATTERMOST_ACCESS.md` has the checklist; a second tunnel replica can run on the same token |
| Heimdall down or wrong during the demo | Fail closed, `/healthz`, runbook, break-glass, tagged releases, rehearsal (D93, D102) |
| Chat platform differences | Text commands are identical on every platform; one Teams quirk keyed on `chat.bridge` (D127) |
| Jira poll misses a ticket | Overlap window, dedupe, reconciler, S20 (D97) |
| FalkorDB license (SSPL) | Fine for self-hosting; matters only if we ever sell a hosted service, then switch to Neo4j Community or Kuzu via Graphiti's drivers |

## 23. Open questions

Whether Teams lands is the only open item. Everything else is decided; T01 to T03 validate it, and a result that contradicts a decision changes this spec in the same commit.
