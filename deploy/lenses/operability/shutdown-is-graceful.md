---
applies_to: []
check: "Does the service finish the requests it has taken and stop taking new ones before it exits?"
proof: "Send a request, stop the service while it runs, and show that the request still gets an answer."
kind: convention
source: heimdall lens template: operability
status: active
---
A service that is stopped mid-request answers with a broken connection, and the client sees a failure that never
happened. A service that stops taking new work too late keeps the queue full of requests it will never serve.

Stop taking new requests, finish the ones in flight, and only then leave. The time this takes is the time the
platform allows before it kills the process, so it has to be shorter than that.
