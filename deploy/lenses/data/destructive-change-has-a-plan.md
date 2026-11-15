---
applies_to: ["**/*.sql", "**/migrations/**", "**/schema/**"]
triggers: ["DROP", "TRUNCATE", "DELETE FROM", "UPDATE"]
check: "Does a change that drops or rewrites data say what happens to the rows that are already there?"
proof: "Run the migration on a copy of the data and show what the old rows become."
kind: convention
source: heimdall lens template: data
status: active
---
A dropped column is gone. A rewritten column has a new value that no other system knows about. In a deploy the old
code may still be running and writing the old shape, so a destructive migration can lose the rows the old code wrote
a minute ago.

A destructive change needs three things: a backup or a copy, a statement of what the old rows become, and a point in
the rollout where the old code is already gone. A reader that still needs the field stays until it has moved.
