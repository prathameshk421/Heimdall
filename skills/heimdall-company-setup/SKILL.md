---
name: heimdall-company-setup
description: Guide a company administrator through a truthful Heimdall installation, VM setup, CLI checks, agent-runtime preparation, first run, and knowledge-base intake. Use when onboarding a new Heimdall deployment or explaining what is supported versus planned.
---

# Heimdall company setup

Use this skill as an onboarding guide, not as permission to invent or execute operational steps. The canonical company-admin guide is [`docs/COMPANY_SETUP.md`](../../docs/COMPANY_SETUP.md). Read it first, then use only the commands and paths it names.

## Operating rules

1. Separate every statement into `supported now`, `manual/unverified`, or `planned/blocking`.
2. Never ask for, print, paste, or commit a secret. Secret values belong in files outside the repository with owner-only permissions. Do not inspect `.env` contents.
3. Do not claim that `admin init`, `setup`, agent enrollment, or knowledge-base file intake works: the current CLI does not implement those commands.
4. Do not create a new CLI spelling for a missing feature. For a gap, describe the desired UX as a proposal and label it `planned/blocking`.
5. Keep the deployment private. The current compose file publishes Heimdall on loopback only; access from a laptop uses the SSH tunnel described in [`docs/RUNBOOK.md`](../../docs/RUNBOOK.md).
6. When a check was not run against the real VM or external service, say `not verified` and give the reason.

## Workflow

1. Read [`docs/COMPANY_SETUP.md`](../../docs/COMPANY_SETUP.md) and confirm the target is a single-company deployment on the team VM, not a multi-tenant service.
2. Inventory prerequisites and security constraints before touching configuration: Docker, SSH access, loopback-only exposure, encrypted storage for production, and owner-only secret files.
3. For the current local/server path, copy [`deploy/heimdall.example.toml`](../../deploy/heimdall.example.toml) outside the checkout, create the secret files named by the config and [`deploy/compose.yaml`](../../deploy/compose.yaml), then use the documented compose health check. Do not put values into TOML or compose environment entries.
4. Use the built CLI surface only:
   - `heimdall doctor [--json] [--strict] [--config PATH]`
   - `heimdall connect list [--json] [--config PATH]`
   - `heimdall connect <kind> [name] [--public-address VALUE] [--config PATH]`
   - `heimdall connect people ...` for the existing people-mapping flow
   - `heimdall cost`, `heimdall budget-check`, `heimdall budget-run`, and `heimdall secrets-sync` where their documented inputs apply
5. Treat an adapter result of `needs_config` with `adapter not built yet` as a product gap, not as a successful connection. The current registry intentionally has not-built entries.
6. Prepare the OpenCode/controller runtime from [`deploy/agents/README.md`](../../deploy/agents/README.md) and [`docs/RUNBOOK.md`](../../docs/RUNBOOK.md), but label host-side enrollment, firewall application, reboot recovery, and live agent execution `manual/unverified` unless evidence is available for the exact installation.
7. Before a first run, verify only observable results: Docker health, `GET /healthz`, the CLI doctor table, and a human-visible ticket/room flow. Do not seed events or call an unsupported knowledge command to make the run look complete.
8. For knowledge-base files, explain the current gap: no implemented CLI intake command or agent-facing upload tool exists. Do not tell an agent to run a proposed command. Record the file's intended owner, team scope, source, retention, and privacy decision separately until the product implements the intake contract.

## Completion report

End an onboarding attempt with:

- files and commands actually used;
- each claim marked `supported now`, `manual/unverified`, or `planned/blocking`;
- checks that passed and their observable output;
- secrets/configuration locations without values;
- open gaps, especially missing `admin init`, missing end-to-end agent setup, and missing KB file intake;
- an acceptance checklist with unchecked items left unchecked.

The skill is complete only when the report is honest about what was not verified. It does not authorize live calls, VM changes, commits, or pushes by itself.
