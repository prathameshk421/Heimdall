---
applies_to: []
triggers: ["http.HandleFunc", "router.", "app.get(", "app.post(", "@app.route", "@router."]
check: "Does every route or handler decide who may call it before it reads or writes anything?"
proof: "Call the new route with no credentials and with another owner's credentials and show what each answer is."
kind: convention
source: heimdall lens template: security
status: active
---
A handler that looks up an object by its id alone trusts the id. Anyone who guesses or reads an id can reach another
owner's data, and nothing in the code says so.

Before the handler touches data, it must have answered one question: who is calling, and may they act on this
object. The answer belongs in the code, not in the hope that the caller checked. A missing object and an object the
caller may not see must answer in the same way, so ids cannot be probed either.
