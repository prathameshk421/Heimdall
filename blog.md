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

### People reach Mattermost through a tunnel (Nov 4)
**Decision.** Teammates use their own Mattermost desktop and phone apps. We publish Mattermost through a named Cloudflare Tunnel on a hostname, keep the VM's only inbound rule as SSH, and set the Site URL and Switch's Public Url to that hostname.
**Why.** Slack, Discord, Telegram and Teams are hosted by their vendors, so every teammate already has an app that reaches them. Mattermost is the one chat server we host ourselves, and its apps need an address to connect to. A tunnel gives one without opening a port. This is the standard way to expose a self-hosted service, and teams that already use Slack or Teams can skip it entirely because Heimdall only names a Switch connection.
**Gave up.** Cloudflare Access in front of it: the Mattermost apps cannot complete its browser login, so we rely on invite-only sign-up, strong passwords and rate limits.
**Needs from the team.** A domain on Cloudflare and a person with VM access to run the tunnel.

### The domain belongs to whoever deploys (Nov 4)
**Decision.** Heimdall never owns or buys a domain. It takes the public address as a setting, and `heimdall connect mattermost` checks it (resolves over HTTPS, answers as Mattermost) and prints the settings to change. For the hackathon the domain is a stand-in; in a real deployment it is the customer's own, exposed however their IT exposes internal tools.
**Why.** The tunnel is free but a domain is not, and no product should hold domains for its customers. Teams that chat in Slack, Discord or Telegram need no domain at all, and Teams needs a public name the customer controls.
**Fallbacks for us.** A free quick tunnel for rehearsals (its address changes on every restart), then Tailscale. We have not confirmed the current student free-domain offer or Tailscale's free user limit.

### Spending $50 on AWS well (Nov 4)
**Decision.** One `m6i.xlarge` (4 vCPU, 16 GB) with swap and an 80 GB disk, stopped whenever nobody is working, running around the clock only for the demo window. This replaces the earlier plan for a 32 GB machine.
**Why.** The account is capped at $50. Using a list price of about $0.19 an hour, the 16 GB machine costs about $4.60 a day plus about $0.35 for disk and the public address, so roughly eight days around the clock fits with a little to spare. A 32 GB machine costs twice that and would use the whole budget in about four days. Dropping Ollama for hosted embeddings is what makes 16 GB workable.
**Avoided on purpose.** A NAT gateway (about $32 a month), a load balancer, interface endpoints, unattached public addresses, extra snapshots, and burstable instances (the `t3` family throttles or bills extra under sustained load).
**Guards.** Budget alerts at $20, $35 and $45, a stop action at $45, and an uptime-based estimate in `heimdall doctor`, because AWS billing data arrives hours late.
**Caveat.** The price comes from a public price table with no region stated, so we confirm the real rate in the account's region.

### How we test, when an AI builds it over many sessions (Nov 4)
**Decision.** Seven layers, cheapest first, and a hard rule: a task is done only when it has run against the real systems, with the evidence written down. The builder follows one loop every time: write the failing test first, make it pass, run the checks, try to break it, record the evidence.
**Why.** A model that forgets between sessions and tends to believe its own work needs tests that remember what the code must do and checks it cannot argue with. Most bugs should die in layers that cost nothing: unit, property and fuzz tests, real Postgres, a real sandbox, and recorded real payloads that are checked against the live systems so they cannot drift.
**Fakes.** The team's rule is "everything real", so test doubles are allowed only where they speed up regression runs, and never as product code. Even the agent in the full-workflow tests is real at the protocol level (it speaks to the real Switch), with only its answers scripted, so the whole flow runs at zero model cost.
**Money.** Testing has a $3 budget. At list prices a full demo run is about 15 to 20 cents, so the budget covers spikes, live smoke tests, one pass of reduced evals and a few rehearsals. It is enforced by separate API keys with their own limits, not by trusting our code, and every live run adds a line to a committed ledger.
**What we found while writing it.** The plan still assumed fake services, a Go-only demo repo, evals on every push (which would spend money on every commit) and four people working in parallel. All were fixed or flagged.
**Gave up.** Full-corpus agent evals, the load test, and testing the setup skill on other people's laptops.

### Test code stays out of the product, and the model is not on a leash (Nov 4)
**Decision.** Two rules. First, test scaffolding (the scripted agent, doubles, recorders) lives only in test code, is excluded from release builds and images, and CI fails if a production binary depends on it. Second, constrain safety, not thought: code is deterministic where correctness or security needs it (the merge decision, evidence by running code, permissions, the sandbox), and the model is free everywhere judgment is needed.
**Why.** The first drafts added fallback after fallback (a provider chain, an advisory gate mode, emergency switches, conditional isolation, an offline embedder) and tried to make the model repeatable (a fixed low temperature, exactly one repair attempt, "the same blocking set three times"). Each adds code that can break, hides the real behaviour, or fights the model. Evidence is verified by code, so a weaker or less repeatable model costs recall, not safety.
**What changed.** The provider fallback chain, advisory mode, the feature kill switches, the no-sudo second design, the gVisor conditional and the Ollama profile are gone. Temperature is no longer forced. A rejected submission goes back to the agent with the errors, and the limit is its time and cost budget, not a count. The rehearsal check is now that each planted issue is found with verified evidence in at least two of three runs, not that three runs match.
**Rule for spikes.** A spike picks one option and the other is deleted.

### Read Switch's own source before inventing infrastructure (Nov 4)
**Decision.** We stopped treating the agent runtime and the Switch deployment as open questions and read Switch's code. Switch already ships a headless agent controller that runs each agent as its own Linux user under systemd, and a standalone Docker Compose with restart policies and a pinned version. We use both instead of building our own supervisor and instead of Console's remote-host onboarding.
**Why.** Console's host process runs under `tmux` and stays down after a reboot until Console relaunches it, and the `!run-cmd` command that our first design relied on was removed by Switch because it did not bring agents online. The controller gives us the per-agent isolation we wanted without code we would have to keep alive.
**Other questions closed by reading.** OpenCode's exact permission keys are in the spec as a working config; Graphiti's search tools already filter by group and by time, so the planned Python sidecar is gone; the chat port needs no capability negotiation, because every approval is a text command; Mattermost's address for the tunnel is set through an environment variable, not the admin console. MFA is probably a paid Mattermost feature, so it is not relied on.
**Left to runtime.** Only validations of decided designs, such as whether an agent really cannot read another agent's files on the real machine.
**Consequence.** Switch will run from Docker Compose, so the friend sets up Docker and the stack instead of Console's host onboarding.

---

## Round 13: what the source said before we wrote any code

**Decision.** A final read of Switch's source and a run of our own guardrails changed several settled decisions. Chat commands are the plain word `heimdall`, because Switch swallows posts starting with `!`. People are matched by their opaque sender id, because `sender_kind` cannot tell a person from an agent. Each agent's token is in its own config file, because sessions do not inherit our variable. Heimdall no longer restarts or resets agents, because sessions are per room and Switch restarts crashed ones.
**What we gave up.** The claim that the harness can stop an agent reading its own folder or posting to its room. Agents can always do both, so we scan their posts and check independence on the board instead.
**Why.** Each assumption was reasonable and none had met the real system. `STAGE_1_FIXES.md` now proves them first, along with fixed hooks: the old guard blocked five out of five normal commands in a test.
**Also fixed.** A PR with no ticket now has somewhere to go, tickets map to repos, test pins are per ticket, and there is one build order.

---

## Round 14: walking the whole plan in order

**Decision.** Rooms now come before tests, because the workflow wakes agents through a room. Live agents arrive before G2 needs one. A deploy step exists from slice 3 instead of slice 7. Local mode runs real agents, because the scripted agent never ships. Five test accounts stand in until teammates take the roles.
**Why.** The earlier order was attractive because it avoided the VM, but the VM now exists, and that order had steps that could never pass where they sat.

---

## Open questions
- Which teammates fill the author, test-author, two reviewer and approver roles, and their GitHub accounts.
- Whether Teams lands before the deadline.
- The AWS instance size, and whether our limited login has sudo (it decides how the agents are isolated).
