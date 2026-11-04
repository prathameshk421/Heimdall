# Heimdall progress

Every session reads this first and updates it before stopping. Status per task is in `TRACKER.md`.

## State

Docs are final and committed on branch `heimdall-build` (not pushed). No product code exists. The VM runs Switch `0.31.0` with Mattermost. Node, OpenCode, the controller and Heimdall are not installed on it.

## Next

1. `STAGE_1_FIXES.md`: T101, T102, T103, T104, then T01, T02, T03.
2. `MVP_SCENARIOS.md` section 7: slice 0 (T00, T10, T16, T18, T98).

## How to resume

Stay on `heimdall-build` in this checkout (`.env` is gitignored, so a new worktree would not have it). Forward ports with the command in `RUNBOOK.md` when a task needs Switch or Mattermost.

## Waiting on the human

Nothing. The GitHub App stays as it is, the key in `.env` is the only OpenRouter key, and a domain is optional (D161).

## Not verified yet

Everything in backlog Part 16 marked from source: the `!` command path live, per-room sessions, the OpenCode permission override, the per-agent token, OpenCode 1.x with Switch 0.31.0. T01 to T03 settle these.

## Failing tests

None yet; there is no code.

## Spend

OpenRouter: about $0 of the $3 testing cap (key limit $4). Read on Nov 4.

## Noticed

Nothing yet.

## Board history

| Date | Task | Story / agents | Green / blocked / red |
|---|---|---|---|
