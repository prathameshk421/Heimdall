# Sample diff: PAY-142, refunds in pay-demo

This is the change the reviewer examples in this folder review. It is the fixture repository
`aarush323/heimdall-fixture-pay`, branch `pay-demo/refunds` into `main`.

- Change id: PAY-142
- Base sha: `a01b2c3d4e5f60718293a4b5c6d7e8f901234567`
- Head sha: `9f1c2ab0c3d4e5f60718293a4b5c6d7e8f901234`
- Acceptance criteria: AC1 a refund cannot exceed what is left of the payment; AC2 a repeated
  `Idempotency-Key` returns the original refund; AC3 a refund is readable only by the merchant that made it.

## Files changed

- `internal/refunds/service.go` (new): the refund service, its request type and its validation.
- `internal/api/refunds.go` (new): `POST /refunds` and `GET /refunds/{id}`.
- `api/openapi.yaml`: the `Refund` schema and the new paths.
- `internal/app/app.go` and `internal/api/api.go`: wiring only.
- `internal/refunds/service_test.go` (new): unit tests for the service.

The commit message says: merchants can refund a payment in full or in part; `POST /refunds` creates a
refund and `GET /refunds/{id}` reads one; a repeated request with the same `Idempotency-Key` returns the
original refund.

## The diff, hunk by hunk

### 1. The cap in `internal/refunds/service.go`, lines 41 to 69

`Create` validates the request, locks the service, looks up the idempotency key, finds the payment, and
then checks the amount:

```
56    if req.Amount > pay.Amount {
57        return Refund{}, false, fmt.Errorf("%w: %d is more than the payment's %d", ErrExceedsPayment, req.Amount, pay.Amount)
58    }
```

The comparison is between this request and the payment. The refunds already made against the payment are
not summed, so two refunds of 60 on a payment of 100 both pass and the merchant refunds 120. A repro test
that sends two refunds of 60 and expects 422 on the second fails with an assertion on head.

### 2. The read in `internal/refunds/service.go`, lines 72 to 79

`Get` returns the refund with the given id and has no merchant argument:

```
72    func (s *Service) Get(id string) (Refund, error) {
75        r, ok := s.store.get(id)
76        if !ok {
77            return Refund{}, ErrNotFound
78        }
79        return r, nil
```

`internal/api/refunds.go` lines 78 to 91 checks only that `X-Merchant-ID` is non-empty before calling
`Get`, so merchant `m-2` reads a refund made by `m-1`. A repro test that creates a refund as `m-1` and
reads it as `m-2` gets 200 and fails its 404 assertion on head.

### 3. The wire format in `api/openapi.yaml`, lines 147 to 168

The `Refund` schema requires `reference` instead of `refund_ref` (line 149), and keeps `refund_ref` only
as a deprecated alias (lines 165 to 168):

```
147    Refund:
148      type: object
149      required: [id, payment_id, merchant_id, amount, currency, reference, created_at]
...
165        refund_ref:
166          type: string
167          deprecated: true
168          description: Deprecated alias of reference, kept in this file for one release.
```

`internal/api/refunds.go` line 28 serves the field as `reference`, and line 40 fills it with the refund id.
A consumer that deserializes `refund_ref` as a required field breaks on the new response.

## What is fine

- Field validation reports every broken rule at once and wraps `ErrInvalid` (lines 82 to 101).
- The idempotency key is scoped to the merchant, and a replay answers 200 with the original refund.
- A new refund answers 201, and an over-payment error maps to 422 in `writeRefundError`.
- `internal/app/app.go` and `internal/api/api.go` only wire the new service and routes.

## Facts the reviewer had

- `mem_refund_cap_invariant` (confirmed): the sum of the refunds of a payment never exceeds the payment.
- `mem_ledger_reads_refund_ref` (confirmed): the ledger deserializes `refund_ref` as a required field.
- `dec_refund_approval_threshold` (confirmed decision): a refund above 10000 cents needs a second approver.
- `pack:consumers/ledger` (the reviewer's own pack): the ledger client and its version.
