# Heimdall

**LGTM is not evidence.** Heimdall makes every merge safe and definite for teams that ship with AI agents. A ticket moves, a chat room opens, failing tests are written before the code, a bad pull request is turned away in seconds, reviewers from different teams each bring the context their team has, a human decides in the room, and the `heimdall/gate` check turns green only on the exact commit that was approved.

Built on [Switch](https://github.com/sandbox-quantum/switch) for rooms and agents, for the Flip the Switch hackathon (Track 01, Best Business Workflow).

## Status

Early build. The design is complete and the code is being written one task at a time (`docs/IMPLEMENTATION_PLAN.md`). Where the build stands is in `docs/PROGRESS.md`; the evidence for each finished task is in `docs/TRACKER.md`.

## About the design documents

The documents under `docs/` (pitch, technical spec, test strategy, decision backlog) were written **before the hackathon started**. The product code in this repository (everything outside `docs/`, `site/` and the root `skills/`) was started after the event opened, and the product's Switch rooms and agents are created by `heimdall admin init` during the event (D61).

## Layout

| Path | What |
|---|---|
| `cmd/heimdall-server` | the server (wiring only) |
| `cmd/heimdall` | the command line: connect, admin, doctor, pack, eval |
| `cmd/archcheck` | fails the build when the import graph breaks the dependency rule |
| `internal/domain` | pure rules and types; imports nothing internal |
| `internal/app` | use cases; imports `domain` and `ports` |
| `internal/ports` | the interfaces the application needs |
| `internal/adapters` | implementations of ports for real systems |
| `internal/transport` | webhook, MCP, web and SSE edges; call `app` |
| `internal/testkit`, `cmd/heimdall-testagent` | **test only**; no production binary may depend on them |
| `docs/` | design, spec, test strategy, progress |

The dependency rule is in `docs/TECH_SPEC.md` section 2.2 and is enforced by `internal/archcheck`.

## Build

You need Go 1.27 and Docker. Pinned tools are in `tools.mod` and run with `go tool -modfile=tools.mod <name>`; nothing is installed globally.

```bash
make tools   # builds and runs each pinned tool, and checks Docker
make ci      # arch check, vet, race tests, govulncheck
```

## For coding agents

Start at `CLAUDE.md`. Rules and the per-task loop are in `docs/harness/AGENTS.md`.
