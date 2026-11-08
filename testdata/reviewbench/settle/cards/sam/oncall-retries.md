---
applies_to: ["internal/worker/**"]
triggers: ["retry("]
check: "Is a retried job safe to run twice?"
kind: invariant
source: session 2026-08-20
---
Worker retries must be idempotent. A retried webhook paid a merchant twice in March.
