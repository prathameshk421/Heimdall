---
applies_to: []
check: "Does every caller of a changed function still get the behaviour it expects?"
proof: "Name the changed symbol, list its callers and show that each one still passes its own test."
kind: convention
source: heimdall lens template: correctness
status: active
---
A change to what a function returns or does reaches its callers without anyone editing them. The caller keeps the old
assumption, so the failure shows up in the caller and looks like a bug somewhere else.

Read every signature and every behaviour change in the diff, take the callers from the change map, and read each one
against the new behaviour. A caller that only looks right because you read the changed code is not a checked caller.
A caller that is not in the change map may still exist: search the tree for the name.
