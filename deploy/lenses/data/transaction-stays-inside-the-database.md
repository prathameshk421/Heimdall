---
applies_to: []
check: "Does the transaction stay inside the database, with no network call, no file write and no wait inside it?"
proof: "List the calls the transaction makes and show that every one of them is to the database."
kind: convention
source: heimdall lens template: data
status: active
---
A transaction holds its locks and its connection while it runs. A call to another service inside it holds both for
the length of that call, and a call that never returns holds them forever.

Keep the transaction short and inside the database. Call the other service before the transaction opens or after it
commits. When the other call has to be inside, the transaction has to be able to give up the whole work, not half of
it.
