# KAN-23: Refund Cap Enforcement

## Description
Ensure refunds cannot exceed the original payment amount and the response identifies the merchant.

## Acceptance Criteria
- **AC1:** A refund request exceeding the original captured payment is rejected with 400 Bad Request.
- **AC2:** Every successful refund response includes the `merchant_id` field.
