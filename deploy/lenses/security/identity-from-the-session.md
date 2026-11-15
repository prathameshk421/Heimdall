---
applies_to: []
check: "Is every decision about the caller's own data made from a value the caller cannot change?"
proof: "Show where the owner, the tenant and the role come from, and show that none of them is read from the request body."
kind: convention
source: heimdall lens template: security
status: active
---
The owner of a request is part of the request's context, not part of its body. A field in the body is chosen by the
caller, so a caller who writes another owner's id into it asks for another owner's data and gets it.

Take the owner, the tenant and the role from the authenticated session or from the token. A body field that looks
like an owner is a finding in itself.
