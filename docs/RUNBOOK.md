# Heimdall Runbook

Operating notes for the team VM. Decisions are D124, D135 and D136 in `BRAINSTORM_BACKLOG.md`. Account IDs, instance IDs and secrets stay out of this repo.

## Switch on the VM

| Item | Value |
|---|---|
| Pinned version | Switch `0.31.0` (newest stable on Nov 4, 2026; upgrades are deliberate) |
| Compose artifact | `ghcr.io/sandbox-quantum/standalone-compose:0.31.0`, manifest `sha256:166a8f6ab77172e4278b402589893f0364271d854ef1c57401371a7909bb0d2c` |
| Compose file SHA-256 | `cba238878078cf3d77fd9e136ef432f1ce5fd1bfe597acd172b9faa6720f0121` |
| Mattermost | Team edition `11.9.0`, pinned by Switch |
| Directory on the VM | `/opt/heimdall/switch` |
| Files | `docker-compose.yml` (unchanged artifact), `docker-compose.override.yml` (restart policies, Site URL), `.env` (mode 600, generated secrets) |
| Profiles | `collab,gateway`, set by `COMPOSE_PROFILES` in `.env` |
| Feature flags | `agent_management`, which requires `CONTROLLER_TOKEN_SECRET` |

All published ports bind to `127.0.0.1` on the VM: gateway `3000`, API `8000`, Mattermost `8065`, Postgres `5432`.

## Connect from a laptop

SSH goes through the `heimdall-vm` alias, which runs over SSM. Keep this running while you work:

```bash
ssh -N -L 3000:localhost:3000 -L 8000:localhost:8000 -L 8065:localhost:8065 heimdall-vm
```

- **Switch Console:** Connect to an existing server, with server address `http://localhost:8000`.
- **Dashboard:** `http://localhost:3000`
- **Mattermost:** `http://localhost:8065`

Print the logins in your own terminal, never in a chat or agent session, then run `clear`:

```bash
ssh heimdall-vm "grep -E 'GATEWAY_ADMIN_(EMAIL|PASSWORD)|MATTERMOST_(ADMIN_)?(USER|PASSWORD)' /opt/heimdall/switch/.env"
```

## Check status

```bash
ssh heimdall-vm 'cd /opt/heimdall/switch && docker compose ps -a'
```

```bash
ssh heimdall-vm 'cd /opt/heimdall/switch && docker compose logs -f --tail=50 switch'
```

`init-db` and `setup` are one-time jobs, so `Exited (0)` is correct for them. The other four services should be `Up`.

## Reboot and recovery

The stack returns by itself after a reboot, in under a minute. This was tested on Nov 4, 2026: the room, its Mattermost channel and the bridge all came back with no manual step.

Two log lines at each start are harmless: `is receiving but is a member of no room`, and one Mattermost `/users/search` session failure while the bridge connects. The directory lookup works once the bridge is up.

## Costs and limits

The setup person owns the AWS account and receives budget alerts at $10 and $25. An automatic budget action stops the VM at $50. If the VM stops unexpectedly, check with them first, because billing data can lag by hours and the stack only returns once the VM is started again.

## Known limits

- **The disk is not encrypted.** Keep only test data and test credentials on this VM. Rotate its secrets before any real use, and use an encrypted volume for a customer install.

- **Mattermost to Switch needs an agent.** The bridge opens no Mattermost socket until agents exist, so with no agents a post stays in Mattermost and does not reach the Switch room. T01 tests this path with a real agent.
- **Open sign-up is on.** Switch's compose file sets `MM_TEAMSETTINGS_ENABLEOPENSERVER` to `true`, which is safe only while Mattermost stays on loopback. Override it before publishing the tunnel (`MATTERMOST_ACCESS.md`).
- **Teams needs one more port.** The compose file publishes only port `8000` for Switch, so the Teams listener on `3978` needs a loopback port in the override before the tunnel in `TEAMS_SETUP.md` can reach it.
- **`t97-reboot-check`** is a test room left from the reboot check. It is safe to archive.

- **`!` posts are Switch commands.** A post starting with `!` that Switch does not know gets "Unknown command" and is not passed on. Heimdall commands start with the plain word `heimdall` (D139).
- **Agent runtime installed Nov 4 (T01 steps 1 and 2 in progress):** Node `22.23.4` from the nodejs.org tarball (SHA-256 checked against its `SHASUMS256.txt`) in `/usr/local`; OpenCode `1.19.5` (`npm install --global opencode-ai@1`, `/usr/local/bin/opencode`); `switch-agent-controller` `0.31.0`, installed with `npm install --global --ignore-scripts` from the GitHub release. That release predates signing (it has no `.sig`, so `install.sh` refuses it), so its SHA-256 `ee62d433...6e47` was checked against the release notes and GitHub's asset digest. Enrolled Nov 4 as `heimdall-vm` (controller id `c230ee62-411b-49d4-bbe2-dca7e2a89bef`) against `http://127.0.0.1:8000`; service `switch-agent-controller-1000.service` from `install-service --separate-users --env-file /etc/switch/agents.env` (16 agent users `sa1000-NN`, group `switch-agents-1000`, agents in `/var/lib/switch-agents/1000`). `/etc/switch/agents.env` must be owned by `ubuntu`: root-owned 0600 made the service exit with "not found". The controller credential is a plaintext file (no OS keychain on the VM).

## Chat connections

| App | Status on Nov 4, 2026 | Notes |
|---|---|---|
| Mattermost | Connected | Private until the tunnel exists; Switch to Mattermost room creation verified |
| Telegram | Connected | Group `Dev_gc` became a room, and messages from two people arrived. Bot replies are checked by direct messaging the bot, and agent replies come with T01 |
| Teams | Not connected | Needs a domain, Azure admin work and port `3978` |

## Upgrade Switch

Read the release notes, back up with `pg_dump`, download the new compose artifact and check its digest, change `SWITCH_VERSION` in `.env`, run `docker compose pull` and `docker compose up -d`, then update this page.

## Agent runtime on the VM (Stage 1, Nov 4)

- **Versions:** Node 22.23.4, OpenCode 1.19.5 (the pin; no other version is installed), `switch-agent-controller` 0.31.0 (hash-checked, see above).
- **Services:** `switch-agent-controller-1000.service` (runs as `ubuntu`), agents `switch-agent-1000@NN.service` as users `sa1000-NN`, `heimdall-agent-firewall.service` (loads `/etc/heimdall/agents.nft`, nftables table `inet heimdall_agents`, matches group `switch-agents-1000`). All return after `sudo reboot` with no step.
- **Files:** `/etc/switch/agents.env` (owner `ubuntu`, 0600: OpenRouter key and `SWITCH_CONTROLLER_SELF_UPDATE=off`), `/opt/heimdall/switch/test-users.env` (five test passwords, 0600). Each agent's harness file is `<agents-dir>/<NN>/<agent name>/opencode.json` on the host (the agent sees it at `/var/lib/switch-agents/1000/agent/<agent name>/`), 0600, owned by the agent's user.
- **Throwaway spike objects:** agents `heimdall-probe`, `heimdall-probe2`, `heimdall-coordinator`; rooms `t01-room-a`, `t01-room-b`, `t02-people`, `t03-lock`; the five test accounts (D158) stay; scripts in `~ubuntu/t01` (including a stub MCP on 127.0.0.1:8080 that is not a service).
- **Traps:** a pending OpenCode `question` blocks that session for good; `auto_mode` must be `true`; the `edit` pattern must be `**/scratch/**`; an agent refuses people who are not its owner until addressing is opened or an identity is claimed; a new agent is added to every default channel; `glob` fails ("ripgrep execution failed").

## Live run of heimdall-server

A live run selects the real Jira, GitHub and Switch adapters, so the server needs a database URL and every credential file before it will start. It refuses to start on the first missing key and names the key in `heimdall.toml`.

**1. Make the secrets directory.** Names only; never paste a value into a chat or a commit. One file per name, each mode `0600` (the config refuses a private key that group or others can read):

| File | Holds |
|---|---|
| `database_url` | the Postgres URL, using the same password as `postgres_password` and host `postgres` inside compose |
| `postgres_password` | the Postgres container's password |
| `openrouter_api_key` | the OpenRouter API key used by Graphiti memory |
| `jira_email` | the Jira account email |
| `jira_api_token` | the Jira API token |
| `github_app_id` | the GitHub App id |
| `github_app_installation_id` | the GitHub App installation id |
| `github_app_private_key` | the GitHub App private key in PEM form |
| `switch_api_key` | the Switch agent API key (from `/agents/register-known`, not a human login) |
| `switch_registration_token` | Switch `AGENT_REGISTRATION_TOKEN` (registers the coordinator) |
| `agent_token_test-author` | MCP bearer for the test-author agent |
| `agent_token_reviewer-1` | MCP bearer for reviewer-1 |
| `agent_token_reviewer-2` | MCP bearer for reviewer-2 |
| `agent_token_reviewer-3` | MCP bearer for reviewer-3 |
| `agent_token_resolver` | MCP bearer for the resolver agent |

Write them under a directory you own, for example `~/heimdall/secrets/`. Check the modes with `ls -l`; every file must show `-rw-------`. Never print the contents. Human Gateway/Mattermost logins are separate from these agent files.


**2. Make the config.** Copy `deploy/heimdall.example.toml` outside the repository (for example `~/heimdall/heimdall.toml`) and set `base_url`, the Switch ids, `project_keys`, the `[agents]` names and the `[[projects]]` entry for the install. Keep `secrets_dir = "/run/secrets"`: inside the container the files above are mounted there. The example already selects `vcs.github`, `tracker.jira`, `chat.switch` and `[agents]`.

**3. Start it.**

```bash
export HEIMDALL_CONFIG=~/heimdall/heimdall.toml
export HEIMDALL_SECRETS_DIR=~/heimdall/secrets
docker compose -f deploy/compose.yaml up -d --build --wait
docker compose -f deploy/compose.yaml ps heimdall-server
```

Only `127.0.0.1:8080` is published. The server has no ingress by design.

**4. Confirm health.**

```bash
curl -sS http://127.0.0.1:8080/healthz
```

The answer is `{"status":"ok"}`. `docker compose ps heimdall-server` shows `healthy` once the healthcheck has passed.

**5. Read the logs.** `docker compose -f deploy/compose.yaml logs -f heimdall-server` shows:

- `changes triggered` — a Jira poll found a status move to In Progress on a human-assigned ticket and started changes. A healthy poll with nothing to do logs nothing, so this line appears only when a ticket moves.
- `ticket poll failed; the next poll retries` — the Jira poll failed; the server keeps the cursor and retries. A healthy poll is the absence of this line.
- `outbox effect failed` — one effect (room open or branch create) failed and will retry; the `kind` and `key` fields name it.
- `outbox drain failed; retrying on the next wake or tick` — a batch could not be claimed or recorded; the loop carries on.
- `outbox drainer stopped` — the drain loop ended; the server is shutting down or the loop hit a fatal error.
- `heimdall-server did not run` — startup failed; the `error` field names the bad key or secret file, never its value.

**6. Stop it.**

```bash
docker compose -f deploy/compose.yaml down
```

`down -v` also removes the Postgres volume; use it only when the event log can be discarded.

## Boot reconciliation (A5)

After the server stops or crashes, nothing is replayed by hand. At boot the server starts the pull request sync and the ticket poll immediately (no wait before the first pass). The sync reads open pull requests, checks, and reviews from GitHub and appends what the event log lacks, through the normal ingest path. The room stream restarts from the durable per-room cursors. A second boot appends nothing, because every append is keyed by its source. Check after a restart: the server log shows `pull requests synced` with `appended` counts, and a repeated boot shows zero.

