# Heimdall company setup

Audience: the company administrator who owns the Heimdall VM and the integrations it connects to. This is a product onboarding guide, not a claim that every planned setup workflow exists today.

## Status key

- **Supported now** — present in this checkout and covered by local tests or a recorded repository workflow.
- **Manual/unverified** — documented or partly implemented, but the exact company install still needs an operator or a real-system check.
- **Planned/blocking** — specified as product work, but not an executable instruction today.

The current CLI is intentionally partial. Its entry point wires `doctor`, `connect`, `cost`, `budget-check`, `budget-run`, and `secrets-sync`; `admin init`, `setup`, and a knowledge-base file command are not implemented. Do not substitute a guessed command.

## Phase 1: prerequisites, VM, and security

### Supported now

Heimdall is designed as one private deployment per organization. The checked-in compose stack contains Heimdall Server, Postgres, FalkorDB, Graphiti, and an optional Ollama profile. The server port is published on `127.0.0.1:8080`; Postgres, FalkorDB, and Graphiti are not published to the host. See [`deploy/compose.yaml`](../deploy/compose.yaml) and [`docs/TECH_SPEC.md`](TECH_SPEC.md).

The team VM's Switch stack is documented in [`docs/RUNBOOK.md`](RUNBOOK.md): it is reached through the `heimdall-vm` SSH alias, with Switch Console/API and Mattermost kept on loopback. A laptop tunnel is:

```bash
ssh -N -L 3000:localhost:3000 -L 8000:localhost:8000 -L 8065:localhost:8065 heimdall-vm
```

The VM runbook records the pinned Switch artifact, directory, ports, reboot behavior, and known limits. Read it before changing the VM.

### Manual/unverified

For a company installation, the administrator must provide the VM, restricted SSH/SSM access, backups, monitoring, and storage protection. The design calls for encrypted storage, no public Heimdall ingress, IMDSv2 hop limit 1, and agent egress restrictions, but this checkout does not provide a single verified company provisioning script. The runbook explicitly notes that the existing test disk is not encrypted; do not treat that VM as a production baseline.

Do not expose port 8080, Postgres, FalkorDB, Graphiti, or Mattermost directly to the internet. Use the documented tunnel for private access. Teams has additional public-hostname and port requirements in [`docs/TEAMS_SETUP.md`](TEAMS_SETUP.md); it is not a prerequisite for a Mattermost-only install.

### Data volume encryption (guidance; not checked by Heimdall)

The data volume that holds the Postgres data and the Heimdall config should be encrypted at rest. Heimdall does not check this and has no `doctor` item for it; the administrator confirms it. How to check:

- Linux with LUKS: `lsblk -o NAME,TYPE,FSTYPE,MOUNTPOINT` should show a `crypt` layer under the mount point of the data volume. `sudo cryptsetup status <mapper-name>` should report `cipher` and `active`.
- AWS EBS: `aws ec2 describe-volumes --volume-ids <id> --query 'Volumes[].Encrypted'` should print `[true]`.

The current test VM's disk is not encrypted. Do not run company data on it. This guidance is not verified on a company host.

## Phase 2: install and deploy the current stack

### Supported now

1. Copy [`deploy/heimdall.example.toml`](../deploy/heimdall.example.toml) to an operator-owned location outside the repository, for example `~/heimdall/heimdall.toml`.
2. Create the secret files named by the TOML and the `secrets:` section in [`deploy/compose.yaml`](../deploy/compose.yaml). The compose file currently requires database, Jira, GitHub, Switch, OpenRouter, and agent-token files. The config stores secret *file names*, not secret values.
3. Set `HEIMDALL_CONFIG` to the copied TOML and `HEIMDALL_SECRETS_DIR` to the directory containing those files. Keep secret files owner-only; the config loader refuses unsafe secret paths/modes and never puts secret values in errors.
4. From the repository root, run the compose workflow already documented by the compose file:

```bash
export HEIMDALL_CONFIG=/path/to/heimdall.toml
export HEIMDALL_SECRETS_DIR=/path/to/secrets
docker compose -f deploy/compose.yaml up -d --build --wait
docker compose -f deploy/compose.yaml ps heimdall-server
curl -sS http://127.0.0.1:8080/healthz
```

The expected health response is `{"status":"ok"}`. `docker compose ... ps` should show the server healthy after its health check. `deploy/verify-compose.sh` is the repository's throwaway compose verification script; it is not a company deployment tool.

`heimdall-server` refuses startup when its real integration configuration is incomplete. Use [`docs/RUNBOOK.md`](RUNBOOK.md), “Live run of heimdall-server”, for the current secret-file inventory, config shape, logs, and stop behavior. Never copy the example values such as placeholder Jira URLs or agent ids into a real deployment.

### Manual/unverified

`make setup` and `heimdall admin init` now create the operator workspace and config and list missing secret files, and `make deploy` installs a signed tag ([`SETUP.md`](SETUP.md), [`../deploy/RELEASES.md`](../deploy/RELEASES.md)). There is still no one-command company installer. Building from source or running the compose stack is therefore an operator-managed/manual path, not a finished company installer.

## Phase 3: CLI connect and doctor

### Supported now

The binary can be built with the repository's existing `heimdall-bin` make target, which writes `.cache/heimdall`; keep that artifact out of commits. The direct source form is useful for a checkout-bound check:

```bash
make heimdall-bin
.cache/heimdall doctor --config /path/to/heimdall.toml
.cache/heimdall doctor --json --config /path/to/heimdall.toml
.cache/heimdall connect list --config /path/to/heimdall.toml
```

`doctor` checks config loading and Docker, reports Postgres as skipped in the current implementation, and checks GitHub permissions only when the GitHub reader is wired. Exit status is non-zero for failing, missing, or not-verified checks; `--strict` also fails on an overly broad GitHub permission.

`connect` never accepts a secret as an argument or reads one from stdin. It names the secret files and writes provider selection after a plan. The current default registry exposes `github`, `file`, `jira`, `mattermost`, and `teams`; not-built adapters report `needs_config` and `adapter not built yet`. The example TOML currently selects `chat.bridge = "switch"`, which does not match that registry, so do not assume `connect list` succeeds against the untouched example: reconcile the config/registry mismatch as a product issue and record it. `connect people` exists for policy mapping, but Switch user listing is not built and the operator may need to type sender ids by hand.

If a dotenv file is already part of an approved local workflow, the implemented helper is:

```bash
.cache/heimdall secrets-sync --env /private/path/to/.env --out /private/path/to/secrets --require
```

It maps only the fixed keys implemented in `cmd/heimdall/secrets_sync.go`, writes mode-0600 files, and never prints values. Do not use it as permission to inspect or share `.env`; project rules prohibit reading or committing secret contents.

### Manual/unverified

The current implementation and tests are not a verified run against every company integration. A `connected` result must come from the adapter's actual check; `adapter not built yet`, `skipped`, and `not verified` are not success. Record the exact config path and check output without recording secret values.

## Phase 4: agent runtime setup

### Supported now

The role skills and OpenCode templates are checked in. [`deploy/agents/README.md`](../deploy/agents/README.md) documents `deploy/agents/install.sh --dry-run`, the agent directory layout, per-agent token file names, `auto_mode: true`, the scratch-only edit rule, and the separation between controller environment and agent files. The role skills currently available are [`skills/test-quality/SKILL.md`](../skills/test-quality/SKILL.md), [`skills/reviewer/SKILL.md`](../skills/reviewer/SKILL.md), and [`skills/resolver/SKILL.md`](../skills/resolver/SKILL.md).

The VM runbook records the test installation's Node/OpenCode/controller versions, service names, Unix-user model, firewall service, and reboot evidence. Treat those as reference evidence for that VM, not as a generic installer.

### Manual/unverified

Controller enrollment, `install-service --separate-users`, systemd service ownership, nftables application, agent token placement, and a real OpenCode task require an administrator on the VM. The local installer script validates/writes agent artifacts; it does not contact Switch, install systemd, or apply nftables. A company must perform and verify those host changes separately.

### Planned/blocking

`heimdall admin init` is the planned resumable workflow that should perform host checks, deploy Switch, enroll the agent controller, create role agents, apply security boundaries, and resume after interruption. It is described in [`docs/TECH_SPEC.md`](TECH_SPEC.md) section 17 and tracked by T99/T27. Today `heimdall admin init` covers the config and the secret-file checklist only; the host, Switch and agent steps are still manual ([`CLI.md`](CLI.md#admin-init)).

## Phase 5: first run

### Supported now

Use the observable stack checks first:

```bash
docker compose -f deploy/compose.yaml ps -a
curl -sS http://127.0.0.1:8080/healthz
.cache/heimdall doctor --config /path/to/heimdall.toml
```

For the VM Switch side, use the status/log/tunnel instructions in [`docs/RUNBOOK.md`](RUNBOOK.md). A real first workflow should show the ticket, room, agent task, tests, reviews, and gate through the external systems. The repository's scenario board is the authoritative product story; run it only through documented Make targets and label missing VM or integration prerequisites as blocked/not verified.

### Manual/unverified

Do not infer an end-to-end green run from a healthy container or seeded event data. The repository history explicitly distinguishes seeded gate runs from live Jira, Switch, agent, and model behavior. A company acceptance run must record which external systems and real agents participated.

## Phase 6: knowledge-base files, ownership, and privacy

### What exists now

The product design defines Graphiti memory, scoped `group_id` queries, provenance, and ingestion from decisions, merged changes, Jira, incidents, chat, and accepted ADRs. The compose stack starts Graphiti and FalkorDB, and the Graphiti configuration uses the OpenRouter secret file. These facts do **not** mean that an agent can upload an arbitrary knowledge-base file through the current CLI.

There is currently no implemented `heimdall knowledge add`, `heimdall kb upload`, or equivalent agent-facing CLI intake command in `cmd/heimdall`. No current agent skill instructs agents to supply KB files, and no verified file-to-episode ownership/privacy workflow exists. Therefore the answer to “how does an agent supply a KB file through the CLI?” is: **it cannot today; this is planned/blocking, not an executable setup step.**

### Proposed UX — not implemented

The desired future flow should be explicit and auditable, for example:

```text
heimdall knowledge add PATH --team TEAM --owner PERSON --source SOURCE --classification CLASS
```

This is a proposal only; do not run it. Before implementation, the product must define and test accepted file types and size limits; redaction and secret/PII scanning before model access; owner approval; team/group scope; source and validity metadata; retention/deletion; deduplication; whether the file is copied, hashed, or converted to episodes; and an agent identity/authorization check. The agent should receive a stable intake result and provenance reference, never unrestricted Graphiti access. Personal context packs must remain separate from shared team memory as specified in [`docs/TECH_SPEC.md`](TECH_SPEC.md) sections 9 and 10.

Until that exists, keep candidate files in an access-controlled company location, obtain the owner and privacy decision outside Heimdall, and do not place them in the repository, secret directory, agent working directory, or compose volume as a substitute for ingestion.

## Acceptance checklist

- [ ] VM access is restricted to the company administrator/team path; no Heimdall or database port is publicly exposed.
- [ ] Production storage encryption, backups, budget alerts, and recovery ownership are documented; the test VM's unencrypted disk is not reused as-is.
- [ ] Config is copied outside the checkout and contains no credential values.
- [ ] Every required secret file is outside the checkout, owner-only, and has a recorded owner; no value appears in tickets, logs, or chat.
- [ ] `docker compose -f deploy/compose.yaml up -d --build --wait` completed for this installation, or the result is marked not verified with its reason.
- [ ] `/healthz` returned `{"status":"ok"}` and service/container status was recorded.
- [ ] `doctor` was run with the real config; failures, skipped checks, and not-verified rows were not silently treated as success.
- [ ] `connect list` was run; each integration is marked connected, manual/unverified, or blocked with the next action.
- [ ] Agent controller enrollment, per-agent Unix isolation, token ownership, `auto_mode`, scratch-only edits, egress restrictions, and reboot recovery were verified on the target VM or marked not verified.
- [ ] A first run used real external observations and records its cost; seeded events are labeled as seeded.
- [ ] Any proposed KB file has an owner, team scope, classification, source, retention decision, and privacy approval recorded outside the unfinished intake path.
- [ ] No one was instructed to run the proposed KB command, and no claim was made that KB file intake exists.

## Sources and scope

This guide adds no new product behavior. It is grounded in [`AGENTS.md`](../AGENTS.md), [`docs/TECH_SPEC.md`](TECH_SPEC.md), [`docs/TEST_STRATEGY.md`](TEST_STRATEGY.md), [`docs/MVP_SCENARIOS.md`](MVP_SCENARIOS.md), [`docs/IMPLEMENTATION_PLAN.md`](IMPLEMENTATION_PLAN.md), [`docs/RUNBOOK.md`](RUNBOOK.md), [`deploy/compose.yaml`](../deploy/compose.yaml), [`deploy/heimdall.example.toml`](../deploy/heimdall.example.toml), [`deploy/agents/README.md`](../deploy/agents/README.md), and the current CLI under [`cmd/heimdall`](../cmd/heimdall).
