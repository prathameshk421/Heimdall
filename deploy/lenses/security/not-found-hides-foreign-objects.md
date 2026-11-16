---
applies_to: []
check: "Does a missing object and an object the caller may not see answer in the same way?"
proof: "Ask for an object that does not exist and for one that belongs to another owner, and show that the two answers are the same."
kind: convention
source: heimdall lens template: security
status: active
---
"Found" and "forbidden" are different answers. A caller who gets "forbidden" learns that the id exists, so they can
map the ids of other people's objects one request at a time.

Both cases answer the same way, with the same status and the same body. A handler that returns 404 for a missing
object and 403 for a foreign one has told the caller that the foreign object exists.
