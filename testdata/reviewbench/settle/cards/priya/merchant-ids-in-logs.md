---
applies_to: ["internal/bank/**", "internal/payout/**"]
check: "Does the log hold a full balance or a customer name? Merchant identifiers alone are fine."
kind: judgement
source: session 2026-10-02
---
I let it go when a log line holds a merchant or account identifier: these are business identifiers, and on-call needs them, and legal has signed this off. I block when a log line holds a person's name or a balance.
