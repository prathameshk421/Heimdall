# Heimdall command reference

Heimdall has three ways in:

1. **The `heimdall` CLI**, for the operator: setup, secrets, integrations, health, cost.
2. **Chat and PR commands**, for people on the team: `heimdall uphold F1` in chat, `/heimdall uphold F1` on GitHub.
3. **Make targets**, for contributors: build, test, deploy.

No command ever takes a secret as an argument. Secrets are files, written by a hidden prompt or from your `.env`.

---

## 1. The `heimdall` CLI

Build it once:

```bash
make heimdall-bin
```

That writes `.cache/heimdall` (git-ignored). `go run ./cmd/heimdall <command>` works too.

Every command that reads config takes `--config PATH`. Without it, Heimdall uses `HEIMDALL_CONFIG`.

### At a glance

| Command | What it does |
|---|---|
| [`admin init`](#admin-init) | Create `heimdall.toml` interactively, or resume setup and list missing secret files |
| [`admin secret set`](#admin-secret-set) | Store one secret file from a hidden prompt |
| [`secrets-sync`](#secrets-sync) | Turn your `.env` into secret files |
| [`connect`](#connect) | List, plan and check integrations: GitHub, Jira, chat, agent runtime, memory |
| [`connect people`](#connect-people) | Map a person to their Switch sender id, GitHub login and reviewer agent |
| [`doctor`](#doctor) | Check the config, Docker, integrations and, with `--host`, the VM |
| [`switch-register`](#switch-register) | Register the coordinator agent in Switch |
| [`controller enroll`](#controller-enroll) | Enroll this machine as a Switch agent controller |
| [`pack refresh` / `pack review`](#pack) | Build, approve and upload a developer's encrypted context pack |
| [`cost`](#cost) | Remaining LLM budget per purpose |
| [`budget-check` / `budget-run`](#budget-check-and-budget-run) | Gate a live model run on the budget ledger |

Exit status is `0` on success, `1` on a failed check or error, and `2` on a bad command line.

### `admin init`

```bash
heimdall admin init --config ~/.config/heimdall/heimdall.toml --secrets-dir ~/.config/heimdall/secrets
```

- **No config yet:** asks for the endpoints and ids, writes `heimdall.toml` with mode `0600`, and lists the secret files it needs.
- **A valid config:** resumes, and prints which secret files are still missing, by name only.

It never asks for, reads or prints a secret value. Default config path: `HEIMDALL_CONFIG`, then `/etc/heimdall/heimdall.toml`.

### `admin secret set`

```bash
heimdall admin secret set --config <toml> --secrets-dir <dir> --name switch_api_key
heimdall admin secret set --config <toml> --secrets-dir <dir> --name github_app_private_key --from-file ./app.pem
```

Reads a single-line value from a hidden prompt, or copies a file with `--from-file`, into `<secrets-dir>/<name>` with mode `0600`. `--secrets-dir` defaults to `secrets_dir` in the config.

### `secrets-sync`

```bash
heimdall secrets-sync --env .env --out ~/heimdall/secrets [--require] [--bootstrap-generated]
```

`make secrets-local` wraps this. Start from [`.env.example`](../.env.example).

| `.env` key | Secret file |
|---|---|
| `OPENROUTER_API_KEY` | `openrouter_api_key` |
| `ATLASSIAN_EMAIL` | `jira_email` |
| `ATLASSIAN_API_KEY` | `jira_api_token` |
| `GITHUB_APP_ID` | `github_app_id` |
| `GITHUB_APP_INSTALLATION_ID` | `github_app_installation_id` |
| `GITHUB_APP_PRIVATE_KEY_PATH` | `github_app_private_key` (the file's bytes are copied) |

| Flag | Meaning |
|---|---|
| `--require` | Fail if any key above is missing. Without it, missing keys are only listed |
| `--bootstrap-generated` | Also create fresh `postgres_password`, `database_url` and `agent_token_<name>` for the five agents. Run it once: a second run replaces them |

It only reads the keys above and never prints a value.

### `connect`

```bash
heimdall connect list [--json]
heimdall connect progress
heimdall connect <kind> [name] [--public-address VALUE]
heimdall connect --check
```

| Form | What it does |
|---|---|
| `list` | Every registered integration and its state |
| `progress` | Where setup stands, step by step |
| `<kind> [name]` | Plan and select a provider. Kinds: `vcs`, `tracker`, `chat`, `agent_runtime`, `memory` |
| `--check` | Run every registered integration's real check |

`--public-address` applies to `chat mattermost`: a public host name, or `"internal only"`. Only an adapter's real check can report `connected`. `skipped`, `not verified` and `adapter not built yet` are not success.

### `connect people`

```bash
heimdall connect people                 # walk through the people in heimdall.yaml
heimdall connect people --name bob --sender <switch-sender-id> --github bob-gh --agent bob-reviewer --roles member,approver
heimdall connect people --list          # list Switch users (not built yet)
```

Edits the `people:` map in the repository's `heimdall.yaml` (`--policy PATH` for another file). A chat command counts only when its Switch sender id is mapped here. Roles: `member`, `author`, `approver`.

### `doctor`

```bash
heimdall doctor [--json] [--strict]
heimdall doctor --host
```

| Flag | Meaning |
|---|---|
| `--json` | Print the checks as a JSON array |
| `--strict` | Treat a too-broad GitHub App permission as a failure |
| `--host` | Check the VM: sudo, systemd, Docker, firewall, resources, reachability of GitHub, Jira and the model provider |

Any failed, missing or not-verified check gives a non-zero exit.

### `switch-register`

```bash
heimdall switch-register --config <toml> --name heimdall-coord-main [--type opencode] [--secrets-dir DIR]
```

Registers the coordinator agent with Switch. The API key Switch returns goes straight into the `switch_api_key` secret file; the command prints only the agent id. Put that id in `[switch] agent_id`.

### `controller enroll`

```bash
heimdall controller enroll --server https://<switch-host> --name heimdall-vm --data-dir /var/lib/switch-agent-controller-data/<uid>
```

Enrolls this machine as Switch's agent controller. It asks for the one-time code without echoing it. Then install the controller service with the same data directory; see [`deploy/agents/README.md`](../deploy/agents/README.md).

### `pack`

```bash
heimdall pack refresh [--profile .heimdall/pack.toml]
heimdall pack review  [--profile .heimdall/pack.toml]
```

| Step | What it does |
|---|---|
| `refresh` | Collects local sources (docs, git history, coding sessions), redacts them twice, distills them and saves a private draft |
| `review` | Shows the redacted draft. On your approval it encrypts it with age and uploads only the ciphertext |

Raw text never leaves the laptop. Exit `2` means the draft is waiting for your approval.

### `cost`

```bash
heimdall cost [--json] [--ledger testdata/cost-ledger.jsonl]
```

Remaining LLM budget per purpose, from the committed ledger. `make cost` runs it.

### `budget-check` and `budget-run`

```bash
heimdall budget-check --purpose e2e --estimate 0.40
heimdall budget-run   --purpose e2e --estimate 0.40 --task T24 --model <model-id> --run-id <id> -- <command>
```

`budget-check` fails when the estimate would pass the purpose's cap. `budget-run` checks first, runs the command, and records the spend in the ledger under a stable run id. Both take `--config` and `--ledger`.

---

## 2. Chat and PR commands

The same grammar works in two places:

| Where | Prefix | Example |
|---|---|---|
| A Switch room, or any bridged chat (Discord, Telegram, Mattermost, Slack) | the plain word `heimdall` | `heimdall uphold F1` |
| A GitHub PR comment | `/heimdall` | `/heimdall uphold F1` |

In chat, `!` and `/` belong to Switch and the platforms, so Heimdall uses a plain word. A leading `@mention` is fine. A command counts only if the sender is mapped in `heimdall.yaml` (see [`connect people`](#connect-people)). Findings are short ids: `F1`, `F2`.

| Command | Who | What it does |
|---|---|---|
| `start <TICKET>` | member | Start a ticket by hand |
| `answer <TICKET> <Q-id> <text>` | member | Answer an agent's question about the ticket |
| `approve-tests` | domain approver | Sign off tests in a sensitive domain |
| `review @a @b @c` | author | Pick the reviewers |
| `uphold <finding>` | approver or domain owner | Confirm a finding. It blocks the merge |
| `dismiss <finding> <reason>` | filing reviewer's person, or approver | Drop a finding. The reason must be at least 15 characters |
| `challenge <finding> <evidence>` | author | Dispute a finding with a test, run id or memory reference |
| `waive-reviewer @x <reason>` | approver | Go on without one reviewer |
| `override <case> <reason>` | approver | Override a named case |
| `spec-change <text>` | approver | Record a change to the ticket's rules |
| `reject <reason>` | approver | Close the change |
| `resume <step>` | approver | Release an escalated step so it runs again |
| `remember <text>` | member | Record a fact in team memory |
| `forget <memory_ref> <reason>` | approver | Invalidate a wrong fact |
| `incident <text>` | member | Record an incident |

The merge approval itself is always a GitHub PR review, by someone who is not the author, on the head commit.

---

## 3. Make targets

`make help` lists them all. The ones you will use:

| Target | Use |
|---|---|
| `make tools` | Prove the toolchain: Go, each pinned tool, Docker |
| `make fast` | Format, vet, dependency rule, lint, unit tests. Under 60 s. Runs in the pre-commit hook |
| `make verify` | Adds race and component tests, rule self-tests, vulnerabilities and secrets. CI runs this |
| `make hooks-install` | Install the pre-commit hook |
| `make heimdall-bin` | Build the CLI into `.cache/heimdall` |
| `make setup` | Create a private operator workspace with a starter `heimdall.toml` |
| `make secrets-local` | Fill `~/heimdall/secrets` from `.env` |
| `make prototype ONLY=P2` | Run one scenario of the product board |
| `make e2e T=T24` | The real-system check for one task. Run `make cost` first if a model is involved |
| `make cost` | Remaining LLM budget |
| `make deploy` | Deploy an exact signed tag to the VM |
| `make restart` / `make recover` | Restart or reconcile the deployed stack, then check health and MCP |
| `make secrets` | gitleaks over the git history |
