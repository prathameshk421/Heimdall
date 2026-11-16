---
applies_to: []
check: "Does the change say how a row that was written by an older version is read by the new one?"
proof: "Show the row from before the change and the code that reads it after the change."
kind: convention
source: heimdall lens template: data
status: active
---
A column that was added with a default has two kinds of rows: the ones the new code wrote and the ones the old code
wrote. The second kind has the default, not the value the business rule gives, and a query that assumes the value
reads the default for every old row.

Name the rows that already exist and what the new code does with them. A default that is not a value the business
accepts is a silent wrong answer for every row written before the deploy.
