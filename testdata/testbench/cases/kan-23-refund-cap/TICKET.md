# KAN-23: Refund Cap Enforcement

## Description
Ensure refunds cannot exceed the original payment amount and the response identifies the merchant.

## Acceptance Criteria
- **AC1:** A refund request whose amount, added to the refunds already accepted for the same payment, exceeds the captured amount is rejected with 400 Bad Request. A running total equal to the captured amount is accepted.
- **AC2:** Every accepted refund (201 Created) response includes the `merchant_id` field, set to the payment's merchant.
