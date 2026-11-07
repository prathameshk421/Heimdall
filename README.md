# Heimdall

**LGTM is not evidence.**

Heimdall is a merge gate for teams that ship code with AI agents. It writes failing tests before the code, reviews each pull request, and sets a GitHub check that turns green only after a person approves.

This is an early build. Some parts ran on the real system. Most parts run on fakes or a local Postgres. The table in [What works today](#what-works-today) shows which is which.

Heimdall is built for the Flip the Switch hackathon (Track 01, Best Business Workflow). It uses [Switch](https://github.com/sandbox-quantum/switch) for chat rooms and agents.

## The problem

Agents write code faster. Pull requests get bigger, and reviews get slower. A reviewer can click Approve after a quick read, and the merge button turns on. Nothing shows that the code works. LGTM is not a reason to merge.

## How it works

1. A person assigns a Jira ticket to a developer and moves it to In Progress. Heimdall starts a change for that ticket.
2. Heimdall opens a chat room for the change and creates a branch named for the ticket.
3. A test author writes tests that must fail on the base branch. The tests come before the code.
4. The developer builds the change and opens a pull request.
5. The slop check rejects a pull request that is too large or has no ticket. A reviewer does not read it.
6. Reviewer agents, one for each team, read the diff and file findings in the room.
7. Each finding names a file and a line. It gives evidence for its claim.
8. A person decides in the room. An approver reviews the pull request on GitHub.
9. The gate sets the `heimdall/gate` check. It turns green only when every rule holds on the approved commit.

## What works today

### Proven on the real system

All three results are on `aarush323/heimdall-fixture-pay` (pull requests #1 and #4).

| What | How it ran | Result |
|---|---|---|
| The server posts a real `heimdall/gate` check | `TestLiveGateOnFixturePR`, with the project's GitHub App, a real Postgres in Docker, and fake Jira and Switch | The check completed with conclusion `failure`. The slop rule failed on the real diff. The merge state became `BLOCKED`. |
| A real non-author approval is read from GitHub | A second GitHub login approved the pull request. `TestLiveGateAfterApproval` read the review through the GitHub adapter. | One approval recorded on the head commit. The gate still lists open rules. |
| The gate goes green on a real pull request | `TestLiveGateGreenPath` on PR #4 (one test under `tests/heimdall/PAY-200/`), with the project's GitHub App and a real Postgres. A second GitHub login approved the head commit. | `heimdall/gate` completed with conclusion `success` ("the merge rule passed"). GitHub shows the PR as clean and mergeable. **The test seeded** the room, task, test, reviewer, review and `heimdall/acceptance` events that agents and the sandbox will produce later. |

### Built and tested on fakes or a local Postgres

These parts do not yet run against the live Jira, Switch or model services.

| Part | Evidence | Limit |
|---|---|---|
| Unit tests and lint | `make fast` exit 0, 3,179 tests, lint 0 issues | Unit level only |
| Event store | Component tests on `postgres:16-alpine` in Docker | No test of a process killed during an append |
| Ticket to room and branch | One room and one branch across a restart, on fakes | The fakes follow the adapter code. Real payloads are not checked. |
| Merge gate on Postgres | `TestMergeGateEndToEnd`, with fakes | Does not reach a success result |
| Jira poll that starts a change | 17 tests with an injected clock | No live Jira call |
| Reviewer and resolver tools | 109 tests in `internal/transport` | No live agent call |
| Scripted test agent | 35 test functions | Runs only against its own fake of the MCP server |
| Local stack in Docker | `deploy/verify-compose.sh` ran to the end on Nov 4 | Graphiti with the OpenRouter embedder is not verified |
| GitHub CI | A re-run of the push run passed on Nov 5 | Covers one commit (`81c098b`) |

### Does not work yet

| Item | State |
|---|---|
| Live Jira calls | Not verified. The poll runs on fakes. |
| Live Switch calls | Not verified. Field names come from the docs, not from the Switch source. |
| Live branch writes (`EnsureBranch`, `CommitFiles`, `Comment`) | Not verified. |
| Live model agents (test author, reviewers, resolver) | Not run. The free model tier ran out. Spend is $0.149 of the $3 cap, with no live model run since. |
| Green gate from the product's own events | Reached once with seeded events (see above). The product does not yet produce the room, test, reviewer and acceptance events end to end. |
| A push that voids an approval, CI from real checks, the `ana` sign-off, the merge | Not run live. |
| Finding severity | Finding events carry no severity. Every finding counts as critical. Only the tier threshold lifts one out of the blocking set. |
| Challenge and uphold commands | No record that they are built. |
| Cost budget | Not enforced. Task T19 is open. |
| Job queue | River is not added. The outbox drainer uses leases. |

The full status for each task is in `docs/PROGRESS.md`. The evidence for each finished task is in `docs/TRACKER.md`.

## Run the checks

```bash
make fast
make verify
```

`make fast` formats the code, runs vet, checks the dependency rule, runs lint and runs the unit tests. `make verify` adds the race tests and the component tests. Those need Docker.

You need Go 1.27 and Docker. Pinned tools are in `tools.mod`. Run `make tools` to build them and to check Docker. On macOS with the newest SDK, the linter build can fail. The Mac lint note in `docs/PROGRESS.md` gives the fix.

## Repository layout

| Path | What it holds |
|---|---|
| `cmd/heimdall-server` | The server. It wires adapters to use cases and starts the transports. |
| `cmd/heimdall` | The command line: `connect`, `doctor` and people mapping. |
| `cmd/archcheck` | Fails the build when the import graph breaks the dependency rule. |
| `internal/domain` | Pure rules and types. Imports no other internal package. |
| `internal/app` | Use cases. Imports `domain` and `ports`. |
| `internal/ports` | The interfaces that the application needs. |
| `internal/adapters` | Implementations of the ports for GitHub, Jira, Switch and Postgres. |
| `internal/transport` | The webhook, MCP and web edges. They call `app`. |
| `internal/platform` | Shared runtime parts: config, clock and ids. |
| `skills/` | Agent skills for the reviewer, the resolver and test quality. |
| `deploy/` | The Docker Compose stack, the server Dockerfile and an example config. |
| `docs/` | Design, spec, test strategy, runbook and progress. |

Test code lives in `internal/testkit` and `cmd/heimdall-testagent`. No production binary depends on them.

The design documents in `docs/` were written before the event opened.

## Run the server against your GitHub App

Follow `docs/RUNBOOK.md`, section "Live run of heimdall-server". It lists the secret files, the config file and the start command. Keep secret files outside the repository. The server refuses to start until every key it needs is set.

## Rules for coding agents

Coding agents start at `CLAUDE.md`. It holds the rules, R1 to R13, and the loop for one task. Each task ends with one commit.
