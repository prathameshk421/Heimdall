---
applies_to: ["**/*.sql", "**/migrations/**"]
check: "Does a backfill of a large table run in batches, so it does not hold a lock on the whole table?"
proof: "Run the backfill against a table with the production row count, and show that a write to that table still succeeds during it."
kind: convention
source: heimdall lens template: data
status: active
---
A backfill in one statement holds its locks until it finishes. On a table with millions of rows that is minutes, and
during those minutes every write to that table waits.

Run the backfill in batches with a key that moves forward, commit between batches, and stop when the batch is
interrupted. The backfill must be able to run again after a failure without doubling the work.
