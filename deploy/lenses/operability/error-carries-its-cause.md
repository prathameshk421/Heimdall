---
applies_to: []
check: "Does every error say what failed and keep the cause, so the person on call can act on it?"
proof: "Show the error text for one failure and the cause it wraps."
kind: convention
source: heimdall lens template: operability
status: active
---
An error without a cause is a sentence without a subject. "invalid request" says nothing at three in the morning:
not which request, not which field, not what the service was doing.

Every error says what failed, in the words of the domain, and wraps the error it came from so the chain reaches the
real failure. The chain is read in the log, so keep the cause and add the context on the way out.
