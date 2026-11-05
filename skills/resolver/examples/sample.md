# Sample case: three disputes on PAY-142 (refunds in pay-demo)

The resolver examples in this folder settle three cases from one change: the fixture repository
`aarush323/heimdall-fixture-pay`, branch `pay-demo/refunds` into `main`, change PAY-142.

- Base sha: `a01b2c3d4e5f60718293a4b5c6d7e8f901234567`
- Current head: `9f1c2ab0c3d4e5f60718293a4b5c6d7e8f901234`
- Previous head: `7c2d9e81b3a4c5d6e7f8091a2b3c4d5e6f708192`
- Acceptance criteria: AC1 a refund cannot exceed what is left of the payment; AC2 a repeated
  `Idempotency-Key` returns the original refund; AC3 a refund is readable only by the merchant that made it.

## Case 1: `case_01HQZX3K8V0M5N7P2R4S6T8W9Y`, finding F1

**Finding F1** (reviewer bob, category `correctness`, severity `high`, confidence `high`): two partial
refunds of 60 on a payment of 100 both succeed, so the merchant refunds 120. Location
`internal/refunds/service.go` lines 56 to 58.

**Reviewer evidence:** `repro_test`, run `run_01HQZX3K8V0M5N7P2R4S6T8WA0` on head
`9f1c2ab0c3d4e5f60718293a4b5c6d7e8f901234`, result `--- FAIL: TestPartialRefundsCannotExceedPayment`,
assertion `second refund of 60 on a payment of 100 = 201, want 422`.

**Memory:** `mem_refund_cap_invariant` (confirmed): the sum of the refunds of a payment never exceeds the
payment. Re-queried at verification, still confirmed.

**Author's challenge:** "the ledger caps refunds before this service sees them". The author cites nothing:
no run, no memory ref, no decision ref.

**Code at head:** `internal/refunds/service.go` line 56 compares `req.Amount` with `pay.Amount` and never
sums the refunds already stored.

## Case 2: `case_01HQZX3K8V0M5N7P2R4S6T8X1A`, finding F2

**Finding F2** (reviewer eli, category `compat`, severity `high`, confidence `high`): the `Refund` schema
no longer requires `refund_ref` and renames it to `reference`, so a consumer that deserializes
`refund_ref` as a required field breaks. Location `api/openapi.yaml` lines 147 to 149.

**Reviewer evidence:** `tool_result`, `oasdiff breaking` on the base and head schema, report at
`api/openapi.yaml` lines 147 to 149. Memory `mem_ledger_reads_refund_ref` (confirmed on 2026-10-20, marked
superseded on 2026-11-03 by `dec_2026_11_03_ledger_v2`): the ledger deserializes `refund_ref` as a
required field.

**Author's challenge:** the ledger client reads `reference` since v2, and the schema still serves
`refund_ref` as a deprecated alias for one release. The author cites:

- `dec_2026_11_03_ledger_v2` (confirmed decision): since ledger v2 the client reads `reference`, and the
  alias `refund_ref` may be removed after one release.
- `mem_ledger_v2_rollout` (confirmed): ledger v2 was deployed on 2026-11-03, before this pull request.

**Code at head:** `api/openapi.yaml` line 149 requires `reference`, and lines 165 to 168 keep `refund_ref`
as a deprecated alias. `internal/api/refunds.go` line 28 serves the field as `reference`.

## Case 3: `case_01HQZX3K8V0M5N7P2R4S6T8X2B`, finding F3

**Finding F3** (reviewer ana, category `security`, severity `critical`, confidence `medium`): any merchant
can read another merchant's refund. `GET /refunds/{id}` checks only that `X-Merchant-ID` is non-empty and
`Get` returns the refund without comparing merchants. Location `internal/refunds/service.go` lines 72 to 79.

**Reviewer evidence:** `repro_test`, run `run_01HQZX3K8V0M5N7P2R4S6T8WB1` on the previous head
`7c2d9e81b3a4c5d6e7f8091a2b3c4d5e6f708192`, result
`--- FAIL: TestRefundReadIsScopedToItsMerchant`, assertion `m-2 read m-1's refund = 200, want 404`.
There is no run on the current head `9f1c2ab0c3d4e5f60718293a4b5c6d7e8f901234`.

**Author's challenge:** the gateway scopes refund reads by merchant since 2026-11-04, so the failure came
from the gateway configuration and not from this code. The author cites `mem_gateway_scopes_reads`
(observed, not confirmed): the gateway scopes refund reads by merchant since 2026-11-04. The author also
says the handler changed after the run.

**Code at head:** `internal/api/refunds.go` line 84 now passes the calling merchant into `svc.Get`, and
`internal/refunds/service.go` lines 72 to 79 still take no merchant and return any refund by id. Nothing in
the repository shows the gateway rule, and the case has no run on the current head.

## What each side could check

| Case | Reviewer's fact | Author's fact | What settles it |
|---|---|---|---|
| F1 | E1 run failed on head | none, a bare assertion | The run and the confirmed invariant |
| F2 | E2 oasdiff report, superseded memory | confirmed decision and confirmed rollout | The decision, because the alias is still served |
| F3 | E1 run failed on the previous head | observed memory about a deploy | Nothing in the case: a fresh run on head, or a person |
