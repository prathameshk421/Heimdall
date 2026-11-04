# Heimdall progress

Every session reads this first and updates it before stopping. Status per task is in `TRACKER.md`.

## State

Docs are final and committed on branch `heimdall-build` (not pushed). No product code exists. T101 to T103 are done and T104 is written (its VM check is T03). The VM runs Switch `0.31.0` with Mattermost, plus Node 22.23.4, OpenCode 1.19.5 and the controller 0.31.0 (installed, not enrolled; versions in `RUNBOOK.md`). Heimdall is not installed.

## Next

1. `STAGE_1_FIXES.md`: T01 from step 2 (write `/etc/switch/agents.env`), then T02, T03.
2. `MVP_SCENARIOS.md` section 7: slice 0 (T00, T10, T16, T18, T98).

## Code layout

Product code lives in top-level folders (`cmd/`, `internal/`, `e2e/`, per T00), never under `docs/`. `docs/harness/` holds only templates that T16 installs at the repo root.

## How to resume

Stay on `heimdall-build` in this checkout (`.env` is gitignored, so a new worktree would not have it). Forward ports with the command in `RUNBOOK.md` when a task needs Switch or Mattermost.

## Waiting on the human

T01 steps 4 and 5 need the VM's `AGENT_REGISTRATION_TOKEN` (coordinator registration) and a gateway admin session (create the managed agent). The permission check refused my reading admin logins from the VM `.env` before, so the human either allows a VM-side script to use them without printing, or does it in the Console. Steps 1 to 3 are done (Nov 4): `agents.env` (owned by `ubuntu`, 0600, because the controller runs as `ubuntu`), controller `c230ee62` enrolled and `switch-agent-controller-1000.service` active with 0 agents.

## Not verified yet

Everything in backlog Part 16 marked from source: the `!` command path live, per-room sessions, the OpenCode permission override, the per-agent token, OpenCode 1.x with Switch 0.31.0. T01 to T03 settle these.

## Failing tests

None yet; there is no code.

## Spend

OpenRouter: about $0 of the $3 testing cap (key limit $4). Read on Nov 4.

## Noticed

Nothing yet. `install.sh` cannot install the controller 0.31.0 (no signature on that release), so it was installed by hand with a checked SHA-256.

## Board history

| Date | Task | Story / agents | Green / blocked / red |
|---|---|---|---|
