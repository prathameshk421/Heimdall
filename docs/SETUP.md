# Set up Heimdall

There are two paths. Pick one.

| Path | You get | You need | Time |
|---|---|---|---|
| [**A. Develop**](#a-develop) | Build, lint and the full test suite on your laptop | Go 1.27, Docker | 5 min |
| [**B. Run it for real**](#b-run-it-for-real) | Tickets, rooms, agents, reviews and the gate on your own repo | A Linux VM, Switch, a GitHub App, Jira Cloud, a model key | An afternoon |

Every command is in [`CLI.md`](CLI.md). Secrets never go in a command line or in chat. They are files with mode `0600`.

---

## A. Develop

```bash
git clone https://github.com/prathameshk421/flip_the_switch_hackathon.git heimdall
```

```bash
cd heimdall
```

```bash
make tools
```

```bash
make hooks-install
```

```bash
make fast
```

```bash
make verify
```

`make tools` proves the toolchain. `make fast` (under 60 s) is the loop you run constantly. `make verify` adds race and component tests and is what CI runs. Contributors and coding agents read [`AGENTS.md`](../AGENTS.md) next.

---

## B. Run it for real

### 0. Get the CLI

```bash
make heimdall-bin
```

```bash
export PATH="$PWD/.cache:$PATH"
```

### 1. What you need

| Thing | Notes |
|---|---|
| A Linux VM | We use AWS `r6a.large`: 2 vCPU, 16 GB. No inbound ports: Heimdall polls GitHub and Jira |
| Docker and rootless Podman | Docker runs the stack; Podman runs the sandbox jobs |
| A Switch server | Rooms, agent identities and chat bridges. Setup on the VM: [`RUNBOOK.md`](RUNBOOK.md) |
| A GitHub App | Installed on your repo, with checks, pull requests and contents |
| Jira Cloud | A project and an API token |
| A model key | OpenRouter, or any provider OpenCode can call |

### 2. Make an operator workspace

```bash
make setup
```

This creates `~/.config/heimdall/` (mode `0700`) with a starter `heimdall.toml` copied from [`deploy/heimdall.example.toml`](../deploy/heimdall.example.toml) and an empty `secrets/` folder. Set `HEIMDALL_HOME=/some/private/path` to put it elsewhere. It never reads or creates a secret.

Edit `heimdall.toml`: the Jira URL and project key, the Switch URL, your repo and base branch under `[[projects]]`. Each key is commented. A value ending in `_secret` is a **file name**, never the secret itself. The server checks every key at boot and refuses to start on any error.

Or answer the questions instead:

```bash
heimdall admin init --config $HOME/.config/heimdall/heimdall.toml --secrets-dir $HOME/.config/heimdall/secrets
```

### 3. Put the secrets in

Copy the template and fill in your own values:

```bash
cp .env.example .env
```

```bash
make secrets-local SECRETS_DIR=$HOME/.config/heimdall/secrets
```

That writes one `0600` file per key and prints no values. `.env` is git-ignored. Then the ones that do not come from `.env`:

```bash
heimdall admin secret set --config $HOME/.config/heimdall/heimdall.toml --secrets-dir $HOME/.config/heimdall/secrets --name switch_registration_token
```

```bash
heimdall switch-register --config $HOME/.config/heimdall/heimdall.toml --secrets-dir $HOME/.config/heimdall/secrets --name heimdall-coord-main
```

```bash
go run ./cmd/heimdall secrets-sync --env .env --out $HOME/.config/heimdall/secrets --bootstrap-generated
```

The first prompts for the Switch registration token without echoing it. The second registers the coordinator and saves its API key for you. The third creates the database password and the five agent tokens: run it **once**, since a second run replaces them.

Check what is still missing (names only):

```bash
heimdall admin init --config $HOME/.config/heimdall/heimdall.toml --secrets-dir $HOME/.config/heimdall/secrets
```

### 4. Map your people

Heimdall listens only to people it knows. In your repo's `heimdall.yaml`, map each person to their Switch sender id, GitHub login and reviewer agent:

```bash
heimdall connect people --name bob --sender <switch-sender-id> --github bob-gh --agent bob-reviewer --roles member,approver
```

A sample policy file is [`testdata/repos/pay-demo/base/heimdall.yaml`](../testdata/repos/pay-demo/base/heimdall.yaml). Policy is always read from the merge base, never from the PR.

### 5. Deploy

```bash
DEPLOY_CONFIG=$HOME/.config/heimdall/heimdall.toml DEPLOY_SECRETS_DIR=$HOME/.config/heimdall/secrets make deploy
```

This installs an exact signed tag on the VM (see [`deploy/RELEASES.md`](../deploy/RELEASES.md)). To try the stack locally first:

```bash
HEIMDALL_CONFIG=$HOME/.config/heimdall/heimdall.toml HEIMDALL_SECRETS_DIR=$HOME/.config/heimdall/secrets docker compose -f deploy/compose.yaml up -d --build --wait
```

```bash
curl -sS http://127.0.0.1:8080/healthz
```

You want `{"status":"ok"}`.

### 6. Install the agents

On the VM, enroll Switch's agent controller, then install the six OpenCode agents, each as its own Unix user:

```bash
heimdall controller enroll --server https://<switch-host> --name heimdall-vm --data-dir /var/lib/switch-agent-controller-data/<uid>
```

```bash
sudo deploy/agents/install-controller.sh --controller-user ubuntu --controller-data-dir /var/lib/switch-agent-controller-data/<uid>
```

```bash
deploy/agents/install.sh --dry-run
```

Details, layout and token files: [`deploy/agents/README.md`](../deploy/agents/README.md). Agent prompts live in [`skills/`](../skills).

### 7. Optional: channels, feeds and cards

- **Standing channels.** Make `heimdall-tests` and `heimdall-reviews` with `POST /admin/rooms` ([`RUNBOOK.md`](RUNBOOK.md)) and put their ids under `[rooms]`.
- **Feeds.** Any extra Switch room (a Telegram group, a Slack or Mattermost channel) under `[[feeds]]` hears every change's story.
- **Personal context cards.** Short files each reviewer reads first: what that person owns, rules that must hold, past incidents. See [`deploy/cards/README.md`](../deploy/cards/README.md).

### 8. Check everything

```bash
heimdall doctor --host
```

```bash
heimdall connect --check --config $HOME/.config/heimdall/heimdall.toml
```

Then move a Jira ticket to **In Progress**. Within seconds you should see a room, a branch and the Test Agent picking it up.

Only a real check reports `connected`. `skipped`, `not verified` and `adapter not built yet` are not success. When something is stuck: [`RUNBOOK.md`](RUNBOOK.md) covers status, logs, reboot, recovery and known limits. The phase-by-phase company checklist is [`COMPANY_SETUP.md`](COMPANY_SETUP.md).
