---
applies_to: []
check: "Is no error thrown away, and does every failure either return or get logged with its cause?"
proof: "List every place the change handles an error and say what it does with it."
kind: convention
source: heimdall lens template: operability
status: active
---
A swallowed error is a failure with no trace. The service keeps serving, the answer looks normal, and the person on
call finds out from the customer.

Every error that arrives is either returned to the caller or written to the log with its cause. When it is
deliberately ignored, the code says so in a comment that names the reason. A fallback that hides the failure needs a
signal of its own, or it is the same as throwing the error away.
