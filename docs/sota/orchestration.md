# Orchestration: the Go server (SOTA Phase 0, Nov 22, 2026)

Scope: rows, reconciler, effects and the outbox, the event store, restart and recovery, concurrency, idempotency,
timeouts, backpressure, observability and failure modes. Sources: `TECH_SPEC.md` 2 to 6, `HARNESS_DESIGN.md` B2 to B8,
the code at `8dfafd3`, and the live runs H11 (KAN-26, `docs/archive/hackathon/LIVE_RUN_H11.md`) and H13 (PRs #11 and #18, PROGRESS H13).

## 1. Desired state

**Truth and decisions.**
- Every fact is one event in one change's hash-chained log; the view is a pure fold, and every decision (row actions,
  `Missing`, the gate) is a pure function of the view plus inputs read fresh for that run.
- A person can always answer "what is this change waiting for, and who must act" from one list (`Missing`), and that
  list holds only things that can still matter for the current head.

**Effects.**
- Every side effect is an outbox row committed with the event that caused it, keyed so that one key is one action
  anywhere (GitHub, Switch, Jira, the runner).
- Each effect kind has a timeout sized to its real work, set where the kind is declared, so a new kind cannot ship with
  a default that is too short.
- A failure is retried with backoff; a repeated failure becomes one need a person can resume; an effect that may or may
  not have happened is never repeated blindly.
- A slow effect never delays a fast one: waking an agent or posting a status never waits behind a 12-minute sandbox job.

**Inputs from chat and GitHub.**
- One bad message or webhook never stops the others. A message that keeps failing is set aside with its error, and the
  sender hears why; every other room keeps flowing.
- Replayed, duplicate and out-of-order deliveries change nothing twice.

**Restart and recovery.**
- A restart at any point loses nothing and repeats nothing that was done: cursors, leases and in-doubt marks make the
  next process continue exactly.
- Deploys are atomic and verified; a bad build is refused before it is installed, and rollback is one command.

**Observability.**
- An operator can see, without SQL: queue depth and age per effect kind, dead and in-doubt jobs, stream position per
  room, the reconciler's last run per change, and model spend per agent per day.
- Every live defect found so far would have been visible on such a page before a person noticed it.

**Performance (see `performance.md`).**
- A change reacts to a new fact within seconds (kick on append), not on the 5-minute sweep; the sweep is only the
  backstop.

## 2. What exists

| Area | What the code does | Evidence |
|---|---|---|
| Event log | Append with an optimistic version check; the store writes the hash chain and verifies it on load | `internal/adapters/postgres/doc.go:3,20`; `rooms.go:125` (`change.VerifyChain`) |
| Fold and decisions | `change.Fold`, `change.Next` (rows), `change.Missing`; pure domain, lint-enforced | `internal/domain/change/rows.go`, `missing.go`; archcheck holds (109 packages) |
| Wake on append | `reconcile.Kicking` wraps the store and kicks the change's reconciler after each append; sweep every 5 min | `internal/app/reconcile/kicking.go:22`, `reconcile.go:89`; `cmd/heimdall-server/app.go:41` |
| Fresh inputs | Policy at the merge base and outside facts cached 60 s; the profile read fresh every run (D179) | `internal/app/reconcile/inputs.go:15,64` |
| Outbox claim | `FOR UPDATE SKIP LOCKED`, lease set in the database clock, oldest first | `internal/adapters/postgres/outbox.go:122-143` |
| Retry | Exponential backoff 5 s · 2^n with ±20 % jitter, capped at 10 min; 8 attempts, then dead | `outbox.go:17-19,95-101` |
| In doubt | A started effect whose lease ran out is never re-claimed; it is a need until `heimdall resume` | `outbox.go:122-124`; `dead_effects.go:20-23` |
| Resume | `heimdall resume <change> <row>` re-arms a row with a new key suffix `/r<n>` (D188 fixed the release) | `internal/domain/change/rows.go` (`jobKey`, `resumeSuffix`); commit `a5f0a00` |
| Timeouts | Default 20 s; own budgets for `test_quality`, `acceptance`, `verify_evidence`, `run_checks` (D187), `profile_trial` | `cmd/heimdall-server/app.go:51-66,618-623` |
| Concurrency | 3 drain workers, batch 1, drain tick 2 s | `app.go:43,46-49` |
| Liveness | Ack 3 min, stall 10 min, probe the provider, one stall wake (D172), fail; cap 2 failures per input (D173) | `internal/domain/liveness/next.go`; `rows.go` `capTask`; live: PR #18 reviewer-2, 21:43 to 21:57 |
| Start-up check | Every row job kind must have an executor or the server refuses to start | `internal/app/outbox/drainer.go` `checkExecutors`; `cmd/heimdall-server/selfcheck.go` |
| Room stream | Durable cursor per room; on any handler error the whole stream stops and restarts from the cursors, backoff 1 s to 1 min | `internal/app/roomstream/stream.go:199-223`; `cmd/heimdall-server/loops.go:119,130-132` |
| Pollers | GitHub PRs every 10 s, trackers every 15 s, agent sync every 10 s | `app.go:37,39`; `loops.go:341` |
| Deploy | `hot-deploy.sh`: database backup, `.prev` copies, hash check of each binary (since `138a7b4`), health wait, rollback | `scripts/ops/hot-deploy.sh` |
| Observability | Structured logs; a dashboard report of changes; no metrics endpoint | `internal/app/dashboard/report.go`; no `/metrics` anywhere (grep) |

## 3. Gaps

Severity: **high** breaks a journey or hides a failure; **medium** costs time, money or trust; **low** polish.

| # | Gap | Severity | Evidence | Smallest correct fix | Test that would have caught it |
|---|---|---|---|---|---|
| O1 | **Fixed Nov 22.** Per-message attempt counts are stored in the cursor table; after `MaxAttempts` (default 3) the message is recorded dead (`switch:msg-dead:...`), `OnDead` logs and notifies the room, the cursor advances and the stream keeps running. Live VM run not verified yet. | done | `internal/app/roomstream/stream.go`, `cmd/heimdall-server/wire_board.go` | — | `TestFailingMessageDeadLettersAfterCapAndStreamContinues` |
| O2 | **Fixed Nov 22.** Two-lane outbox draining: `ports.KindClaimer` + Postgres `ClaimKinds` + `outbox.Config.Kinds`; server runs 3 quick-lane workers and 3 sandbox-lane workers separately. Docker component tests and live VM run not verified yet. | done | `internal/adapters/postgres/outbox.go`, `internal/app/outbox/drainer.go`, `cmd/heimdall-server/app.go`, `loops.go` | — | `TestSlowSandboxEffectsDoNotBlockQuickTaskAssign` |
| O3 | **Fixed Nov 22.** `EffectKinds()`, `EffectKind.Budget()`, and `EffectKind.IsSandbox()` declare timeouts beside each effect kind in `internal/domain/change/effect.go`; `cmd/heimdall-server/app.go` builds `KindTimeouts` from them and fails startup if any kind lacks a budget. | done | `internal/domain/change/effect.go`, `cmd/heimdall-server/app.go` | — | `TestEveryEffectKindHasABudgetAndSandboxKindsCoverRunnerTime` |
| O4 | **Fixed Nov 22.** `deadNeeds` in `internal/domain/change/missing.go` skips head-scoped dead jobs (`run_checks`, `verify_evidence`, `acceptance`, `github_check`) whose key does not match the PR's current `HeadSHA`. | done | `internal/domain/change/missing.go` | — | `TestDeadJobForOldHeadIsNotANeedOnNewHead` |
| O5 | **In-doubt effects wait for a person even when the action is safe to repeat.** Every kind is treated as unsafe; a room post or a GitHub check with an idempotent key could be retried. | low | `outbox.go:122-124` | Mark kinds whose external key makes a repeat harmless; re-claim those after the lease | Outbox test: an idempotent in-doubt kind is re-claimed, a non-idempotent one is not |
| O6 | **Fixed Nov 22.** `GET /admin/ops` reports outbox depth and oldest age per kind, dead and in-doubt counts, dead chat messages, open changes, and the per-role scorecard (`ports.OpsSnapshot`, `postgres/ops.go`, `cmd/heimdall-server/operator_ops.go`). Live DB query not verified yet. | done | `cmd/heimdall-server/operator_ops.go`, `internal/adapters/postgres/ops.go` | — | `TestOperatorOpsReportsQueuedAndDeadJobsByKindAndScorecard` |
| O7 | **A closed PR is noticed up to 5 minutes late.** `OpenPRs` lists only open PRs, so a close is found by the reconcile pass. | low | PROGRESS Noticed H11 (close 19:11:39, `pr.closed` 19:15:36) | List recently closed PRs too, or use the GitHub webhook the design already has | prsync test: a PR closed between two polls is recorded on the next poll |
| O8 | **Fixed Nov 22.** `PROpened` after an unmerged `PRClosed` clears `v.Closed` in `change.View` and is matched by `reopenedAfterSQL` in Postgres `OpenChanges`/`OpenPRStreams`, resuming the change on PR #2 while keeping merged PRs final. Docker SQL test not verified yet. | done | `internal/domain/change/view.go`, `internal/adapters/postgres/open_changes.go` | — | `TestSecondPROpenedAfterUnmergedCloseResumesChange` |
| O9 | **The stream's sender-identity read is per message.** Each command resolves people from the policy at the merge base; a slow GitHub read delays every room. | low | `internal/app/collaboration/commands.go:113-122` | Cache the directory per change and base like the policy | Benchmark, not a unit test |
| O10 | **Deploy of a new runner can take the stack down.** The compose unit depends on the runner; a runner that fails to start fails compose (Nov 21, 15:19 and 19:06). | medium | PROGRESS Noticed H13; `heimdall-compose.service` dependency | The hash check now refuses bad copies; also start the runner first and roll back automatically when its health fails, before touching compose | Script test with a runner stub that exits 1: the old runner is restored and compose is not restarted |
| O11 | **Fixed Nov 22.** The gap said `make verify` had not run since H12. Wrong: `.github/workflows/ci.yml` runs `make ci` (= `make verify`, with Docker) on each push. Run 44987109073 (`dab504b`) passed unit, race and component (4,481 tests), lint self-test, hooks, harness drift, scripts and govulncheck. It failed only at `secrets`: 4 gitleaks `generic-api-key` findings, made-up values in the P-AM15 action tests on the `product-build` branch (CI fetches all branches). The 4 exact fingerprints are now in `.gitleaksignore`; `make secrets` locally: 833 commits, no leaks. The real gap was a red CI that nobody read. | done | CI run 44987109073; `.gitleaksignore` | Keep CI green; read the run after each push. |

## 4. Order of work

1. O11 (CI for `make verify`): done Nov 22. CI already ran it; only the secrets step was red.
2. O1 and O2: the two ways one slow or bad input stops everything.
3. O6: make the next defect visible without SQL.
4. O3 and O4: remove the two failure classes that recurred.
5. O8, O10, then the low ones.
