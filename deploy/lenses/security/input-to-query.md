---
applies_to: []
triggers: ["Query(", "Exec(", "execute(", "raw(", "rawQuery", "cursor.execute"]
check: "Does every value that came from outside the program reach the query as a parameter?"
proof: "Send a value that would change the query if it were pasted into it, and show that the query still means one thing."
kind: convention
source: heimdall lens template: security
status: active
---
A query built by pasting a value into its text runs whatever the value says. The value comes from outside the
program, so the person who sends it chooses the query.

Every value that came from a request, a file, a queue message or a database row belongs in a parameter or a bound
value, never in the text of the statement. The identifier of a table or a column may be built from a fixed list; a
value may not.
