---
applies_to: ["**/*.sql", "**/migrations/**", "**/schema/**"]
triggers: ["ALTER TABLE", "CREATE TABLE", "DROP TABLE", "CREATE INDEX"]
check: "Is every migration in this change safe to run while the old code is still serving?"
proof: "Run the migration against a database that holds rows the old code wrote, and show that the old code still works."
kind: convention
source: heimdall lens template: data
status: active
---
During a deploy the old code and the new schema are both live. A migration that the old code cannot survive stops
the writes, and a migration that the new code needs before it is deployed stops the reads.

An additive change comes first: a new column that is nullable or has a default, an index that does not lock, a new
table. The code that uses it comes second. The change that removes the old shape comes third, and only when the old
code is gone.
