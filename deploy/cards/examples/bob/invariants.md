# Bob (payments owner): rules that must hold

- Refund amounts are summed across partial refunds. The cap is compared with the payment amount minus refunds already made, never per request. Two refunds of 60 on a payment of 100 must not both succeed.
- A refund is idempotent by Idempotency-Key (ADR-004): a repeat from the same merchant returns the original refund with 200, never 409.
- Money amounts are int64 in the smallest currency unit. Never float.
