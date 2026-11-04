# Heimdall progress

Every session reads this first and updates it before stopping. Status per task is in `TRACKER.md`.

## State

Docs are final and committed on branch `heimdall-build` (not pushed). No product code exists. Stage 1 is finished (T101 to T104, T01 to T03; evidence in `TRACKER.md`, findings in backlog D163). The VM runs Switch `0.31.0` with Mattermost, plus Node 22.23.4, OpenCode 1.19.5, the enrolled controller 0.31.0 and the agent egress firewall (`RUNBOOK.md`). Heimdall is not installed.

## Next

1. Stage 1 is done.
2. `MVP_SCENARIOS.md` section 7: slice 0 (T00, T10, T16, T18, T98). Product code goes in top-level folders, not `docs/`.

## Code layout

Product code lives in top-level folders (`cmd/`, `internal/`, `e2e/`, per T00), never under `docs/`. `docs/harness/` holds only templates that T16 installs at the repo root.

## How to resume

Stay on `heimdall-build` in this checkout (`.env` is gitignored, so a new worktree would not have it). Forward ports with the command in `RUNBOOK.md` when a task needs Switch or Mattermost.

## Waiting on the human

Nothing. T01 is done (Nov 4); spike results are in `TRACKER.md`. The test agents `heimdall-probe` and `heimdall-probe2`, rooms `t01-room-a` and `t01-room-b`, and the coordinator watcher in `~ubuntu/t01` on the VM are throwaway.

## Not verified yet

Everything in backlog Part 16 marked from source: the `!` command path live, per-room sessions, the OpenCode permission override, the per-agent token, OpenCode 1.x with Switch 0.31.0. T01 to T03 settle these.

## Failing tests

None yet; there is no code.

## Spend

OpenRouter: $0.149 of the $3 testing cap (key limit $4), read on the VM after T03, Nov 4.

## Noticed

Nothing yet. `install.sh` cannot install the controller 0.31.0 (no signature on that release), so it was installed by hand with a checked SHA-256.

## Board history

| Date | Task | Story / agents | Green / blocked / red |
|---|---|---|---|
