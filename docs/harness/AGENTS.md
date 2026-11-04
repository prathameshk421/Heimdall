# Heimdall: rules for coding agents

You are building Heimdall one task at a time. You lose memory between sessions, so everything you need is in files. Read in this order, then stop reading and start: `docs/PROGRESS.md` (where the build is and what is next), this file, `docs/MVP_SCENARIOS.md` section 7 (the one build order), the task's row in `docs/IMPLEMENTATION_PLAN.md`, and the spec sections it cites. Precedence when docs disagree (D138): the highest-numbered backlog part from Part 8 on > `TECH_SPEC.md` > earlier backlog parts; `MVP_SCENARIOS.md` and `TEST_STRATEGY.md` govern testing, `CODING_STANDARDS.md` governs code. Product code lives in top-level folders (`cmd/`, `internal/`, `e2e/`), never under `docs/`.

## Commands (never invent others)

| Command | Use |
|---|---|
| `make fast` | format, vet, lint, unit tests. Under 60 s. Run constantly |
| `make verify` | adds component and contract tests. Before every commit that ends a task |
| `make prototype ONLY=P2` | the product board for one scenario. Run before and after your task |
| `make e2e T=T24` | the real-system check for a task. Run `make cost` first if an LLM is involved |
| `make lint-selftest` | proves each lint rule still fires (fixtures in `testdata/lint`) |
| `make harness-test` | every hook case; `make harness-install` copies `docs/harness/` into place and `make harness-install CHECK=1` fails on drift |

## The loop (one task per session, one commit per task)

1. Run `make prototype ONLY=<scenario>` and keep the red line.
2. Write the failing test. Run it. Read why it fails. It must fail on an assertion.
3. Write the smallest code that passes. Keep `make fast` green.
4. `make verify`, then the real check. Try to break your own work once.
5. Write the evidence (commands, exit status, key output lines, cost, what is **not verified**) into the tracker row and `docs/PROGRESS.md`.
6. Commit `T##: what`. Stop.

## Rules (each has an id so a hook or reviewer can cite it)

- **R1 Red first.** No production code without a test you saw fail for the right reason.
- **R2 Never weaken a check to get green.** Do not skip, delete, loosen or `//nolint` a test, scenario, lint rule, timeout or hook. If a check is wrong, say why in the commit and ask. Edits to `.golangci.yml`, `e2e/scenarios`, `e2e/stories`, `testdata/real` and this file need the human's yes (the harness asks).
- **R3 Dependency rule.** `domain` imports nothing internal; `app` imports `domain` and `ports`; adapters implement ports; `transport` calls `app`. Lint enforces it.
- **R4 No test code in production, no speculative fallback, no scripted model reasoning** (`CODING_STANDARDS.md` section 12).
- **R5 Pure domain.** No I/O, clock, randomness or logging in `internal/domain`. Time and ids come through ports.
- **R6 Errors.** Wrap with context, never swallow, never panic outside `main` and tests. `errors.Is` and `errors.As` for branching.
- **R7 No `time.Sleep`, no `fmt.Print*`, no `os.Getenv` outside `internal/platform/config`, no package-level mutable state, no `init()` that does work.** Lint enforces it.
- **R8 Secrets.** Never read, print, log or commit `.env`, `secrets/`, tokens or private keys. Live tests get credentials only through the e2e simulator.
- **R9 Real or "not verified".** If something could not run on the real system, write `not verified` with the reason. Never write "should work".
- **R10 Scope.** One task, one concern. Do not refactor unrelated code. Anything else you notice goes into `docs/PROGRESS.md` under "Noticed".
- **R11 Git.** Never `--no-verify`, never force-push, never push unless the human asks, never rewrite history, never commit a failing test without saying so.
- **R13 No cuts, no hacks (D162).** Do not drop or reduce any task, step or check unless the owner explicitly says so. No temporary shortcuts or workarounds; implement it cleanly and correctly. If blocked, stop and ask.
- **R12 Stuck.** After two failed attempts at the same error, stop and write what you tried and what you think is wrong into `PROGRESS.md`. Do not try a third variant of the same idea.

## Things you cannot infer from the code (add new ones as you find them)

- Tests reach the app only through the declared seam over HTTP. A route that does not exist is a 404 assertion failure (a good red). A compile error is a JUnit `<error>`, which is rejected.
- GitHub approvals count only from a login that is not the PR author and only on the head commit. Two test logins exist (`aarush323`, `rekdfr`). Always pass the token explicitly per call; never rely on the active `gh` account.
- Live objects are in the simulator's run manifest (label `heimdall-test`, PR branches `heimdall-test/<run-id>`, product branches `<KEY>-*` for its tickets). Never touch anything else. Live PRs target `heimdall-demo`, never `main`.
- Chat commands start with the plain word `heimdall` (`heimdall uphold F1`). Switch swallows `!` posts, and platforms take `/` as their own. GitHub comments use `/heimdall`.
- A command counts only if its Switch sender id is in `people:`. `sender_kind` cannot tell people from agents. Agents always hold Switch's room tools, so never assume a room is agent-free.
- People never see a ULID. Findings are `F1`, `F2` in chat.
- Policy is read from the merge base, never the PR head.
- The Jira poll re-queries from `cursor - 10 min` and dedupes by (key, changelog id).
- Each VM agent's `opencode.json` holds its token. Never read, print or copy it. The OpenCode model id is `openrouter/deepseek/deepseek-v4-flash`.

## Access and branch

- The VM is `ssh heimdall-vm`; forward ports as in `docs/RUNBOOK.md`. Never paste VM logins into a chat. Switch's source, for checking facts, is `~/.t3/projects/switchthon/switch`.
- Commit on `heimdall-build` as `T##: what`. Do not push to this repo unless the human asks. For GitHub checks use `aarush323/PBL_TOS_RISK_ANALYZER` (base `heimdall-demo`, never `main`; branches `heimdall-test/<run-id>`) and `aarush323/heimdall-fixture-pay`.
- Before any live model run, state the expected cost; the testing cap is $3. Anything not run is written "not verified".
- If a real result contradicts a decision, change the decision and the spec in the same commit. Do not add a fallback.

## Where to look

`docs/CODING_STANDARDS.md` (code rules), `docs/TEST_STRATEGY.md` (layers and budget), `docs/MVP_SCENARIOS.md` (the board), a package's `doc.go` (its design note and traps; start from `docs/harness/doc.go.template`).
