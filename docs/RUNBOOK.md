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
- **Agent runtime installed Nov 4 (T01 steps 1 and 2 in progress):** Node `22.23.4` from the nodejs.org tarball (SHA-256 checked against its `SHASUMS256.txt`) in `/usr/local`; OpenCode `1.19.5` (`npm install --global opencode-ai@1`, `/usr/local/bin/opencode`); `switch-agent-controller` `0.31.0`, installed with `npm install --global --ignore-scripts` from the GitHub release. That release predates signing (it has no `.sig`, so `install.sh` refuses it), so its SHA-256 `ee62d433...6e47` was checked against the release notes and GitHub's asset digest. Not enrolled and no service yet.

## Chat connections

| App | Status on Nov 4, 2026 | Notes |
|---|---|---|
| Mattermost | Connected | Private until the tunnel exists; Switch to Mattermost room creation verified |
| Telegram | Connected | Group `Dev_gc` became a room, and messages from two people arrived. Bot replies are checked by direct messaging the bot, and agent replies come with T01 |
| Teams | Not connected | Needs a domain, Azure admin work and port `3978` |

## Upgrade Switch

Read the release notes, back up with `pg_dump`, download the new compose artifact and check its digest, change `SWITCH_VERSION` in `.env`, run `docker compose pull` and `docker compose up -d`, then update this page.
