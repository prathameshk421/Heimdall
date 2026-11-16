# KAN-42: Payout Idempotency

## Description
Payouts must be strictly idempotent under `Idempotency-Key` header.

## Acceptance Criteria
- **AC1:** Initial request with a unique key creates a payout record and returns 201 Created with a `payout_id`.
- **AC2:** A repeated request with an identical key returns the stored response (same status and body) without creating a new payout.
- **AC3:** Request missing `Idempotency-Key` header returns 400 Bad Request.
