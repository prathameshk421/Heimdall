---
applies_to: []
check: "On every path where the work fails, does the code return the error instead of a zero value?"
proof: "Force the failure in a test and assert that the error comes back instead of an empty result."
kind: convention
source: heimdall lens template: correctness
status: active
---
A failure that becomes an empty result is a failure the caller cannot see. The caller stores nothing, sends nothing
and reports success, and the data is lost with nothing in the logs.

Follow every error return in the change and every place a value is produced. Where the code logs the error and
continues with a zero value, decide whether the caller can tell the difference. If it cannot, the change has moved
the failure, not fixed it.
