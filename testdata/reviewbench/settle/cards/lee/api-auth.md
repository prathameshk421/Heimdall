---
applies_to: ["internal/api/**"]
triggers: ["http.handlefunc"]
check: "Does each new route check the caller owns the object?"
kind: invariant
source: commit 19ce2aa
---
Every route checks object ownership.
