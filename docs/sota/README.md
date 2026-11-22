# SOTA Phase 0: desired state and gaps (Nov 22, 2026)

One file per area (`SOTA_QUALITY_PLAN.md` Phase 0). Each has the desired state, what exists with evidence, and a gap
table: severity, evidence, smallest fix, and the test that would have caught it. Claims not backed by a run or a line of
code are marked `not verified`.

| Area | File | Gaps |
|---|---|---|
| The Go server | `orchestration.md` | O1 to O11 |
| Agent triggering | `agent-triggering.md` | T1 to T8 |
| Agent reasoning | `agent-reasoning.md` | R1 to R7 |
| Lenses and review quality | `lenses.md` | L1 to L7 |
| The whole journey | `product-function.md` | F1 to F7 |
| The person's experience | `ux.md` | U1 to U7 |
| Performance and cost | `performance.md` | P1 to P7 |

## Order of work across areas (Phase 1 input)

1. **O11** `make verify` in CI with Docker: done Nov 22. CI already ran it on each push; it was red only on 4 fake test keys in gitleaks, now ignored by exact fingerprint.
2. **U1** post every Ask to the person who must act; **F1** write back to the tracker; **F2** close the room with a summary: done Nov 22 (`make fast` 4,390 tests, 0 lint issues; live VM/Jira/Switch run and Docker component tests not verified yet).
3. **O1** one failing chat message must not stop every room; **O2** slow sandbox jobs must not block wakes and posts: done Nov 22 (`make fast` 4,392 tests, 0 lint issues; Docker component tests and live VM run not verified yet).
4. **L1, L2** one defect is one finding across heads and seats (50 filings for 8 issues on PR #18); this also cuts
   **P1, P2** most of the review cost: done Nov 22 (cross-seat and cross-head merge on submit with consensus reporter tracking; live VM run not verified yet).
5. **O6** an operator page (queues, dead jobs, stream lag), **P4** spend per agent and a daily cap, **R6** a quality
   scorecard per role.
6. **T1** the lead agent for plain-words questions (also **U6**); **T2** ticket edits re-open the tests.
7. Phase 2 suite for what never ran live: the resolver (**R1**, **F3**), a clean PR (**L5**), a planted race (**L4**),
   sign-off, approval invalidation and force-push (**F6**), every platform (**T3**, **T7**, **U7**).
