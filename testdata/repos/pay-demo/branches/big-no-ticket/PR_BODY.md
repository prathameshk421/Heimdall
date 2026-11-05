## What

Adds webhook delivery. Merchants can subscribe a URL to payment and refund events.

## Details

- Every delivery is signed with the subscription secret (HMAC-SHA256 over the timestamp and body).
- Network errors, 429 and 5xx answers are retried with capped exponential backoff.
- Other 4xx answers are final and recorded.
- Subscriptions and deliveries are scoped to the calling merchant.

## Testing

Unit tests cover signatures, backoff, retry policy and merchant scoping. The API has HTTP tests.

No ticket for this one yet.
