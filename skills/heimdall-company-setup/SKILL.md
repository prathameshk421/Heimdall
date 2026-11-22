---
name: heimdall-company-setup
description: End-to-end onboarding and connector setup guide for Heimdall across Action Mode (zero-infra GitHub/GitLab PR gating) and Server Mode (VM, Switch, Telegram/Slack/Discord/Mattermost/Teams, Jira, GitHub App, OpenCode agents, and memory cards). Use when setting up Heimdall for a company or troubleshooting connectors.
---

# Heimdall Company & Connector Setup

Use this skill to onboard a company cleanly from zero to verified end-to-end PR gating and multi-platform chat orchestration. Always use the real `heimdall` CLI commands below and verify every configured platform before declaring setup complete.

## Security & honesty rules

1. **Never print, paste, log, or commit secret values.** Store secrets in owner-only files (`chmod 0600`) outside the git checkout, or pass them via stdin (`--model-key-stdin`) / GitHub Environments (`heimdall`).
2. **Keep Server Mode private.** `deploy/compose.yaml` and `deploy/lite/compose.yaml` bind to loopback (`127.0.0.1`); access from laptops uses SSH port forwarding (`docs/RUNBOOK.md`).
3. **Never fake a pass.** Every check you didn't run live against the real service must be reported as `not verified` with the reason.

---

## Step 1 — Choose the deployment mode

| Mode | When to use | Footprint |
|---|---|---|
| **Action Mode** (fastest) | Zero-infra GitHub PR proof gating in shadow or enforce mode | No VM; runs on GitHub Actions (`deploy/action/`) |
| **Lite Server Mode** | Small VM (<256 MiB RAM), Postgres + `heimdall-server` + `heimdall-runner` | `deploy/lite/compose.yaml` |
| **Full Server Mode** | Multi-agent rooms (Switch + Telegram/Slack/Discord/Mattermost/Teams), Jira polling, Graphiti memory | `deploy/compose.yaml` + `deploy/agents/` |

---

## Step 2 — Path A: Action Mode (2-minute GitHub PR Gate)

1. **Bootstrap the repository** (creates `heimdall` GitHub Environment, `HEIMDALL_LOG_KEY`, `HEIMDALL_MODEL_KEY`, and shadow workflows):
   ```bash
   heimdall action init -repo OWNER/REPO -apply -model-key-stdin < /path/to/model_key
   ```
   For enforce mode with a dedicated GitHub App, add `-enforce` (and `-app-code CODE` after confirming the manifest).
2. **Add `heimdall.yaml` on the default branch** (policy is always read from the merge base):
   ```yaml
   action:
     mode: shadow   # flip to enforce after reviewing weekly digest
     break_glass: [lead-login]
     budget:
       per_pr_usd: 1.00
       monthly_usd: 50.00
   ```
3. **Optional OSS analyzers & GitLab CI**:
   - GitHub SARIF analyzers (`gosec`, `Trivy`): copy `deploy/ci/github-analyzers.yml` into `.github/workflows/`.
   - GitLab CI: adapt `deploy/ci/gitlab-ci.yml`.
4. **Verify live**: Open or push to a test PR, confirm `Heimdall trigger`, `Heimdall proof`, and `Heimdall gate` succeed, and confirm the PR summary comment posts. Check weekly readiness with:
   ```bash
   heimdall action digest -repo OWNER/REPO
   ```

---

## Step 3 — Path B: Server Mode & All Platform Connectors

### 3.1 Base configuration & secrets
1. Generate `heimdall.toml` interactively (mode `0600`, outside the repo):
   ```bash
   heimdall admin init --config /etc/heimdall/heimdall.toml
   ```
2. Install required secret files (`chmod 0600`) under `secrets_dir` (never inline in TOML or `.env`):
   - `database_url`, `openrouter_api_key`
   - GitHub App: `github_app_id`, `github_app_installation_id`, `github_app_private_key`
   - Jira: `jira_email`, `jira_api_token`
   - Switch: `switch_registration_token` (`switch_api_key` is written automatically by `switch-register`)
3. Start services:
   - Full stack: `docker compose -f deploy/compose.yaml up -d`
   - Low-RAM (<256 MiB) VM: `docker compose -f deploy/lite/compose.yaml up -d`
   - Verify `GET http://127.0.0.1:8080/healthz` returns `200 OK`.

### 3.2 Connect every platform via `heimdall connect`
Inspect all registered connectors first:
```bash
heimdall connect list --config /etc/heimdall/heimdall.toml
```
Then connect each category your company uses:

- **VCS (GitHub)**:
  ```bash
  heimdall connect vcs github --config /etc/heimdall/heimdall.toml
  ```
- **Issue Tracker (Jira or local file)**:
  ```bash
  heimdall connect tracker jira --config /etc/heimdall/heimdall.toml
  ```
- **Chat Platform via Switch (`telegram`, `slack`, `discord`, `mattermost`, `teams`, `switch`)**:
  ```bash
  heimdall connect chat telegram --config /etc/heimdall/heimdall.toml   # or slack | discord | mattermost | teams | switch
  ```
  Complete these 4 steps so chat mentions work smoothly on every platform:
  1. **Switch Console bridge**: Create the platform connection in Switch Console (Telegram bot from `@BotFather` with group privacy disabled if needed; Slack app invited to channel; Discord bot added to server/channel; Mattermost or Azure Teams bridge per `docs/TEAMS_SETUP.md`).
  2. **Register coordinator**:
     ```bash
     heimdall switch-register --config /etc/heimdall/heimdall.toml --name heimdall-coord-main
     ```
  3. **Enroll & install headless controller + attach room session**:
     ```bash
     heimdall controller enroll --server https://SWITCH_URL --name heimdall-vm --data-dir /var/lib/heimdall-controller
     sudo deploy/agents/install-controller.sh --controller-data-dir /var/lib/heimdall-controller
     ```
     **Critical QoL check**: Verify the coordinator (`heimdall-coord-main`) has an active OpenCode/controller session attached to every target platform room (e.g. `telegram: Dev_gc`, Slack `#eng-reviews`, Discord `#prs`) so users never see `"I don't have a session connected to this room"`.
  4. **Map human senders** (required so `heimdall uphold/dismiss/waive` commands in chat are authorized — `sender_kind` alone cannot separate people from agents):
     ```bash
     heimdall connect people --config /etc/heimdall/heimdall.toml
     ```
- **Agent Runtime & Memory**:
  ```bash
  heimdall connect agent_runtime switchroom --config /etc/heimdall/heimdall.toml
  heimdall connect memory graphiti --config /etc/heimdall/heimdall.toml
  ```

### 3.3 Team Memory Cards (laptop-side redaction + DSSE signing)
```bash
heimdall cards --help
```
Scan local engineering notes/sessions, automatically scrub secrets & PII (`internal/domain/redact`), review/approve candidates, and sign approved cards with DSSE Ed25519 envelopes before sharing to the repository.

---

## Step 4 — Full Verification Gate (before finishing)

Run all of the following and include their observable output in your completion report:

1. `heimdall connect --check --config /etc/heimdall/heimdall.toml` (every selected connector reports `connected`).
2. `heimdall doctor --strict --config /etc/heimdall/heimdall.toml` (all host, Docker, and integration checks pass).
3. `curl -sf http://127.0.0.1:8080/healthz` (Server Mode).
4. **Live smoke test on each configured platform**:
   - **GitHub**: push a PR and confirm summary comment + status check.
   - **Chat (Telegram / Slack / Discord / Mattermost / Teams)**: mention `@heimdall-coord-main` in the target room and confirm it responds (not `"I don't have a session connected to this room"`), and test a `heimdall` command from a mapped human sender.
   - **Jira** (if enabled): confirm ticket poll picks up a test issue.

Report any unchecked item explicitly as `not verified` with the exact reason.
