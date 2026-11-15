---
applies_to: []
check: "Does every log line carry a level and the id of the request it belongs to, and no secret or personal datum?"
proof: "Show the log line the change writes for one request and name the request it belongs to."
kind: convention
source: heimdall lens template: operability
status: active
---
A log line without a request id cannot be tied to the request that produced it, and a log line at the wrong level
hides the failure inside the noise. A log line with a token in it hands the token to everyone who can read the log.

Every line has a level that says how bad it is, and the id of the request or the job it belongs to. Nothing that
would let a reader act as a user goes into the line.
