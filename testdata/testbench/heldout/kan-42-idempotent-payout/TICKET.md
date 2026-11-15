# KAN-42: Payout Idempotency

## Description
Payouts must be strictly idempotent under `Idempotency-Key` header.

## Acceptance Criteria
- **AC1:** Initial request with unique key creates payout record and returns 201 Created.
- **AC2:** Repeated request with identical key returns stored response without creating new payout.
- **AC3:** Request missing `Idempotency-Key` header returns 400 Bad Request.
