---
applies_to: []
check: "Does an answer that goes back to the caller hide the stack, the statement and the internal names?"
proof: "Trigger the failure and show that the body the caller receives names no table, no file path and no stack."
kind: convention
source: heimdall lens template: security
status: active
---
An error message that carries the internal names tells the caller how the system is built: the tables, the paths, the
services and sometimes the credentials. It is free reconnaissance for the next attempt.

The message that leaves the process names the failure in plain words. The detail belongs in the log, where the people
who run the service can read it and the caller cannot.
