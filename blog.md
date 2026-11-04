# Building Heimdall: decisions and why

A running log for the Flip the Switch hackathon (Nov 4 to 7, 2026) and the Best Blog track. Each entry says what we decided, why, and what we gave up. New entries go at the bottom. Nothing here is edited after the fact except to fix a mistake, and then the fix is noted.

Heimdall is a merge-safety layer for pull requests that runs on Switch. A Jira ticket becomes failure-first tests, reviewer agents bring each person's context, and a deterministic gate decides whether the PR can merge.

---

## Product shape

### Heimdall is a server, not a laptop tool (Nov 4)
**Decision.** Heimdall and Switch run together on one Linux VM. People never touch that machine. They use their own chat app, GitHub and Jira.
**Why.** The gate has to be always on: it polls GitHub and Jira, runs agents and posts a check. A laptop server is only reachable by its owner and sleeps. Switch's own deploy docs make the same split between a personal local server and a team server.
**Gave up.** The convenience of a one-click local run. We keep a "local mode" clip for evaluation only.

### Switch is self-hosted, because nothing else is offered (Nov 4)
**Decision.** We run our own Switch server. Switch Cloud shows as "coming soon" in Console, and the hackathon page says Switch is free to self-host and lists no hosted instance.
**Why.** It is the only option that exists. It also fits the story: team context stays with the team.
**Detail worth knowing.** A server Console installs can start its own Mattermost, so we do not install a chat server ourselves.

### Mattermost first, Teams later (Nov 4)
**Decision.** Build and demo on Mattermost. Teams is an adapter we add afterwards.
**Why.** Switch's docs call Teams the most involved platform to connect: Microsoft pushes messages in, so it needs a public HTTPS address, an Azure app registration, an Azure Bot, admin-consented Graph permissions and a certificate. Mattermost opens its connection outward and needs nothing public.
**Gave up.** Teams in the first demo.

---

## Language and architecture

### Go for everything we write (Nov 3)
**Decision.** The server, CLI and tooling are Go. The dashboard is server-rendered HTML with htmx, so there is no Node build step.
**Why.** One static binary is easy to ship and run on a VM. Goroutines make the pollers and workers cheap. `go:embed` puts the dashboard inside the binary. Tests are fast. It also matches the team's preference: no Node, no npm tooling for our own code.
**Gave up.** The ecosystem convenience of TypeScript for the dashboard. If a frontend ever needs more than htmx can give, TypeScript is allowed there (decided Nov 4). The server, CLI and every performance-critical path stay Go.
**Note.** Switch's own agent controller is a Node program. It is part of Switch, not our code.

### Ports and adapters, with a dependency rule (Nov 3)
**Decision.** `domain` is pure and imports nothing internal. `app` uses `domain` and `ports`. Adapters implement ports. A CI check enforces it.
**Why.** The merge rules and the gate become plain functions we can test with plain values, and every external system (GitHub, Jira, Switch, memory, sandbox) can be replaced without touching the rules.

### Plug-and-play integrations and a `heimdall connect` CLI (Nov 4)
**Decision.** Adapters register themselves in a small registry (name, kind, required config, a read-only `Check()`). `heimdall.toml` picks them by name. `heimdall connect` lists what exists, prompts for what is missing, tells the admin which secret file to create, and checks the result.
**Why.** Adding Teams, or another tracker, should be one package and one line. It also makes setup a product experience rather than a README.
**Rule.** Secrets are never accepted as command arguments.

### Event sourcing with a transactional outbox (Nov 3)
**Decision.** Every change is a sequence of events with a version number. Side effects (a GitHub check, a room post, a Jira write) are written as outbox rows in the same transaction as the event that causes them.
**Why.** Nothing external happens without a committed event, and every committed effect eventually happens exactly once. A crash cannot leave us having told GitHub something we never recorded.

---

## Latency and polling

### Poll GitHub and Jira, no public ingress (Nov 3)
**Decision.** GitHub is read every 10 seconds and Jira every 30. No webhook, tunnel, domain or open port. Webhook mode stays as an option.
**Why.** A server with no public surface is much easier to secure, and it removes a whole category of setup for anyone installing Heimdall. At the polling rates we chose, usage stays well inside GitHub's rate limit.
**Gave up.** A few seconds of reaction time. A webhook would be near instant. For a merge gate that waits on humans and CI anyway, ten seconds is not felt.
**Latency targets we hold ourselves to.** A webhook, if enabled, is acknowledged in under 200 ms at p99, because it only verifies, stores and enqueues. The slop rule runs before any agent, so a junk PR is rejected in seconds and costs no tokens.

---

## Agents and models

### OpenCode for the agents on the VM (Nov 3 to 4)
**Decision.** The test-author, reviewer and resolver agents run on OpenCode.
**Why.** Switch supports Claude Code, Codex and OpenCode as agent tools. Claude Code is only officially supported with Anthropic models, and the team uses no Anthropic API. OpenCode talks to OpenRouter directly. We looked at Pi and found no Switch support.
**Lockdown.** Each agent gets its own harness: only Heimdall's MCP tools, no shell, no web, edits only in a scratch folder, and network rules per Unix user. Even if an agent setting is wrong, the network rule still holds.

### One cheap model, one OpenRouter key (Nov 3 to 4)
**Decision.** `deepseek/deepseek-v4-flash` for every role. Test spending is capped at $3 on a single key.
**Why.** At roughly nine cents per million input tokens and eighteen per million output, a long review costs well under a cent. Using the same model for the "generic reviewer" baseline keeps the comparison honest: any difference we show comes from context and gating, not a stronger model.
**Gave up.** The per-role keys with separate limits that the design wanted. We have one key, so the cap is shared.

### The gate is deterministic (Nov 3)
**Decision.** Agents propose findings. Code decides. The merge rule, evidence checks and approvals are plain Go, and every approval is bound to a commit SHA.
**Why.** A language model should not be the thing that says a PR is safe. It can find problems and argue. The decision must be reproducible.

---

## Sandbox

### Not Python only, and not a fixed language list (Nov 4)
**Decision.** Heimdall detects the repo's shape, an agent proposes a profile (image, build and test commands, services), a person reviews it, and Heimdall validates it before the sandbox runs it. The first draft of the spec shipped only Go, Node and Python images, and we are replacing that.
**Why.** Real repos come in every shape. Our own test repo has a Python backend, a React frontend and Docker Compose.
**Safety.** The agent only proposes. Heimdall enforces allowlisted images, no network during execution, and resource limits.

---

## Integrations

### GitHub App signs with the Client ID (Nov 4)
**Decision.** The GitHub App authenticates with a signed token whose issuer is the Client ID, as GitHub now recommends. Polling needs no webhook.
**Why.** Least moving parts. A webhook with no reachable address only produces failed deliveries.
**What went wrong.** Our first install had no permissions and then too many. We now keep a short list: Checks, Contents, Pull requests and Issues (write), Metadata (read), and we check the real permissions with a script rather than trusting the settings page.

### Jira through its REST API (Nov 4)
**Decision.** The Go server talks to Jira directly with a project-scoped token. No wrapper CLI or MCP server in the product.
**Why.** Fewer layers and no Node dependency. For our own development we use plain REST calls.

---

## Process

### Real everything, no fakes (Nov 4)
**Decision.** Development starts when the real Switch server, Mattermost, GitHub App and Jira are up. We do not build against fake adapters.
**Why.** Fake adapters hide the exact problems the hackathon is about: how agents behave in real rooms, with real permissions, over real latency.

### Stage 4 is where it becomes a product (Nov 4)
**Decision.** The most important stage is the product experience and agent quality: `heimdall setup` and `heimdall connect`, platform choice, credential prompts, clear errors, `doctor`, and evals that show the same blocking set three runs out of three.
**Why.** Judges and users meet the setup before they meet the gate.

---

## Reliability review (Nov 4)

### AWS, with headroom, never stopped on a schedule
**Decision.** One EC2 instance, 8 vCPU and 32 GB preferred (4 and 16 is the floor, with swap), encrypted disk, SSH only, no idle-stop schedule while we demo.
**Why.** The first design estimated 11 to 13 GB of a 16 GB machine, with no swap. One out-of-memory kill would take the merge gate down. The extra cost for four days is small next to a broken demo.
**Also.** Instance metadata is locked down (IMDSv2, hop limit 1, blocked for agents and sandbox), because on AWS anything on the VM could otherwise read the instance role's credentials.

### Hosted embeddings instead of Ollama
**Decision.** Memory embeddings come from OpenRouter (`voyage-4-lite`, 1024 dimensions, $0.02 per million tokens). Ollama stays as an optional profile.
**Why.** We tested it live: the call works with the same key and returns 1024 dimensions, so the memory config does not change. It frees one to two gigabytes and a CPU-bound container on the machine that matters most.
**Gave up.** Fully offline memory by default.

### Untrusted code never runs next to the secrets
**Decision.** A separate `heimdall-runner` under its own user, with a rootless runtime (gVisor if it passes our escape tests), runs every PR-derived job. The main server never holds the Docker socket.
**Why.** A PR author's code was going to run on the same machine as Switch and every token, one Docker permission away from root. A merge-safety tool cannot be the weakest link.

### Secrets on disk, not in memory
**Decision.** Secrets live in `/etc/heimdall/secrets` on the encrypted disk and appear as Docker secrets inside containers.
**Why.** The first plan used `/run/secrets`, which is memory-backed. Every reboot, or the idle-stop schedule we also planned, would have wiped it.

### Narrowed GitHub tokens
**Decision.** Polling uses a read-only token. Each write mints a token limited to one repository and one permission.
**Why.** We tested it: GitHub honours the narrowing exactly. The first design held contents-write on every repo and relied on our code to behave.
**What went wrong first.** Our app showed no permissions, then more than we asked for, and the settings page was not the source of truth. We now read the real permissions from the API.

### Jira polling that cannot miss a ticket
**Decision.** Each poll re-reads from ten minutes back and deduplicates by ticket and change id. Descriptions are parsed from Atlassian Document Format.
**Why.** Jira searches resolve to the minute and the index can lag, so a plain "changed after the last time I looked" can skip a ticket. We also confirmed that Jira returns descriptions as a JSON tree, not text, so the acceptance criteria need a real parser.

### Chat adapts to the platform
**Decision.** The chat port reports what each platform can do (threads, cards, buttons, mentions) and Heimdall always also posts the plain text command. `!heimdall` is the chat syntax everywhere.
**Why.** Teams, Mattermost and Telegram differ, and a platform that cannot show a button must still let someone approve. Approvals need a mapped person and the exact commit.

### A generic sandbox, written by an agent and checked by code
**Decision.** Each repo gets a profile (image, setup, test command, services such as a throwaway Postgres, evidence tools). An OpenCode agent drafts it, a person merges it through a PR, and Heimdall validates it before anything runs.
**Why.** Real repos come in every shape, and our own test repo has a Python backend, a React frontend and Docker Compose. The agent proposes, the validator decides. Go-only checks became defaults of the Go profile.

### Rules for a project that must not break
**Decision.** Pin versions and images, deploy only tagged releases, one command to deploy and one to recover, kill switches in policy, an advisory mode for the gate, rehearsals three times, a two-hour soak test, backups and a recorded fallback video.
**Why.** It is a hackathon: the demo is the product on the day. Every dependency has a documented way to degrade, and the list of what we will cut first, and what we never cut, is written down.

### Code quality is a deliverable
**Decision.** `docs/CODING_STANDARDS.md`, a short design note per package written before the code, strict linting, and a review pass at the end of each stage.
**Why.** "Clean, readable code without slop" only happens if the rules are written down and checked.

---

## Open questions
- Which teammates fill the author, test-author, two reviewer and approver roles, and their GitHub accounts.
- Whether Teams lands before the deadline.
- The AWS instance size, and whether our limited login has sudo (it decides how the agents are isolated).
