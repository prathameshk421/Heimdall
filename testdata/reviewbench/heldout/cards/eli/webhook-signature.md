---
applies_to: ["internal/signer/**", "internal/delivery/**"]
triggers: ["crypto/hmac", "SignPayload"]
---
# Webhook Signature Standard

All external webhook recipients require HMAC-SHA256 signatures in `X-Signature-SHA256`.
Downgrading to SHA-1 or MD5 is rejected by upstream banking receivers (ADR-019).
