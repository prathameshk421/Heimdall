# Shadow digest: `acme/pay`

Period: 2026-11-16 to 2026-11-23

## Shadow digest

13 pull requests in this period. Checked: 12. Not checked: 1. Shadow: 13. Enforce: 0.

- would pass: 6 of 12 (50.0%)
- would block: 6 of 12, of which 3 would pass once the missing facts were known
- not checked: 1 of 13 had no result from either check
- readiness: the check is not ready to enforce: some pull requests would have blocked on a real finding

Top would-block causes:

- proof unknown: `no_result`
- fast check unmet: `approval`
- fast check unmet: `secrets`
- fast check unknown: `diff_coverage`
- proof blocked

Proof runs: 5 (1 blocked, 2 unknown, 1 not reproduced, 0 skipped trivial).

Candidate findings: 4 proven (1 blocking, 3 need a person), 4 dropped, 10 not checked.
Break-glass: 1 open, 1 reviewed.

Cost: $2.08 in total over 13 pull requests, mean $0.16, p50 $0.16, p95 $0.38.

## What we did not check

Clauses not checked, by clause:

- `memory_rules`: 1 pull request

Clauses not checked, by reason:

- no reason given: 1 pull request

Candidate findings not checked, by reason:

- budget hit: 4 candidates
- turn cap reached: 2 candidates

Proof runs that ended unknown, by reason:

- no result: 2 runs

Candidate findings dropped, by reason:

- no test reproduced it: 2 candidates
- refuted: 1 candidate
- no location: 1 candidate

Fast check wall time: p50 40.0s, p95 40.0s, max 40.0s, mean 40.0s, over 12 runs.

Proof run wall time: p50 2m 0s, p95 2m 0s, max 2m 0s, mean 2m 0s, over 5 runs.

A digest is a summary of recorded results. It never decides a merge, and a pull request without a result is not a pass. These pull requests ran in shadow mode, so no pull request was blocked by these numbers.
