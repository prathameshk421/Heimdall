# Performance and cost (SOTA Phase 0, Nov 22, 2026)

Scope: latency and cost per row, queue depth, token spend, runner time, database growth and leak risks. Sources:
`TECH_SPEC.md` 1 (targets), the live runs H11 (KAN-26) and H13 (PRs #11 and #18), the VM database and the agents'
OpenCode databases on Nov 21 to 22, and the code at `eba4b9e`. Costs are estimates from token counts at the H11 rates;
the provider balance was never read.

## 1. Desired state

| Measure | Target |
|---|---|
| Slop decision on GitHub after a PR opens | p95 under 15 s (`TECH_SPEC.md` 1, D155) |
| A new fact reaches its row | under 5 s (kick on append) |
| Wake to ack, healthy agent | under 1 min |
| One review of a small PR (under 100 changed lines) | under 10 min wall time, under $0.05, under 20 tool calls |
| Verifying the findings of one head | in parallel with other work, under 3 min for 15 findings |
| A sandbox job never blocks a wake or a status post | always (see `orchestration.md` O2) |
| Spend | visible per agent, per change and per day; a hard cap per day that pauses assignment, not the server |
| Storage | bounded by retention: logs and blobs expire by class; the event log grows only by facts |

## 2. Measured

| Measure | Value | Evidence |
|---|---|---|
| PR open to slop | 11 s (PR #18 created 20:22:37, `slop.evaluated` 20:22:48) | GitHub, event log |
| Slop to pick | 75 s (pick wait 1 min by design) | PR #18 events 2 and 6 |
| Profile request to proposal | 21 s; trial about 30 s | PROGRESS H13 revision |
| Review on head `90f85fae` | tasks 21:17, reviews by 21:57 (40 min, one stall included) | PR #18 events 648 to 683 |
| Tool calls per review | about 33 (98 to 113 per reviewer over three heads) | `tool_call` table, 20:20 to 22:10 |
| Findings on a 20-line change | 50 filed, 33 verified, over three heads | PR #18 event counts |
| Verification | 14 findings one after another, 22:00 to 22:05 | events 685 to 698 |
| Tokens for PR #18 | reviewers 625k input, 13.3M cache read, 254k output and reasoning | OpenCode databases since 19:45 UTC |
| Cost for PR #18 | about $0.50 to $0.60, about $0.06 per review | estimate at H11 rates |
| Database | 10 MB, 705 events, 326 outbox rows after two days of live runs | `pg_database_size`, counts |
| Server data volume | 3.7 MB | `du` on the VM |
| Runner | one job at a time, priority order | `internal/adapters/rootlessrunner/queue.go:25` |

## 3. Gaps

| # | Gap | Severity | Evidence | Smallest correct fix | Check that would have caught it |
|---|---|---|---|---|---|
| P1 | **Reviews read far more context than they use.** 13.3M cache-read tokens for one PR: each review re-reads about 1.4M tokens of session context; caching keeps it cheap today, but it is most of the spend and grows with each head. | high | OpenCode token counts above | New session per review task (Switch reset is not available, D172; use a fresh room session per head), and a context budget in `get_task` that drops prior-head material | A spend check in the Phase 2 suite: tokens per review on a fixed PR, compared run to run |
| P2 | **Too many findings for a small change.** 50 filed on a 20-line change across three heads; verification of all of them runs one at a time. Noise costs reviewer tokens, runner time and the reader's attention. | high | PR #18 event counts; see `lenses.md` | The spec merges overlapping findings from different seats (Jaccard ≥ 0.6, `TECH_SPEC.md` 566); **not verified** whether it ran on PR #18 (3 seats filed the planted bug as F32, F40, F47, each verified separately). Merge before verification, cap findings per seat per head, verify only findings that could block | Phase 2 precision score on planted bugs |
| P3 | **One runner job at a time.** Verification, checks, trials and red-first runs share one queue; a busy runner delays every change on the VM. | medium | `queue.go:25`; 14 verify jobs in 5 min | Two runner slots on the current VM size, measured first; priority already exists | Load test: two changes' verify jobs finish in about half the time |
| P4 | **Fixed Nov 22 (cap & Missing).** `In.SpendCapExceeded` pauses new agent task assignments and names the cap in `Missing`; `reconcile.Config.Spend` feeds today's spend vs cap. Live Switch usage feed not verified yet. | done | `internal/domain/change/rows.go`, `missing.go`, `internal/app/reconcile/reconcile.go` | — | `TestSpendCapPausesTaskAssignmentsAndNamesCapInMissing` |
| P5 | **Fixed Nov 22.** `blobfs.Store.SweepExpired` deletes blobs older than each time-expiring class's retention (`DefaultRetention`: 90 days for `log`, `diff`, `bundle`; preserves `evidence`, `pack`, `knowledge`). | done | `internal/adapters/blobfs/sweep.go` | — | `TestSweepExpiredDeletesOldLogsAndKeepsFreshAndEvidence` |
| P6 | **The profile is read from Postgres on every reconcile run** (D179 removed the cache because it hid proposals). Two indexed reads per run is fine now; it grows with changes times runs. | low | `internal/app/reconcile/inputs.go:64-67` | Invalidate on the profile store's own writes instead of a time cache | Benchmark |
| P7 | **Confirmed Nov 22.** `rowGate` already skips emitting when `g.Same(in.View)` holds; verified with `TestGateRowDoesNotEmitSecondCheckWhenResultIsUnchanged`. | done | `internal/domain/change/rows_review.go:303`, `rows_gate_unchanged_test.go` | — | `TestGateRowDoesNotEmitSecondCheckWhenResultIsUnchanged` |

## 4. Order of work

1. P4 (spend view and cap): needed before any larger automated suite runs.
2. P2 and P1: the two biggest token costs, which also improve review quality.
3. P3, P5, then P7 and P6.
