---
applies_to: []
check: "Does the change do what the ticket asked and nothing else?"
proof: "Write the ticket's acceptance criteria and mark each one with the lines of the change that satisfy it."
kind: convention
source: heimdall lens template: correctness
status: active
---
A change that does more than the ticket asks is two changes in one pull request: one that is reviewed and one that
is not. The second one is where the bug hides.

Map every acceptance criterion to the lines that satisfy it. Then read the diff for the parts that satisfy no
criterion. A refactor, a rename, a dependency bump and a new flag all belong in their own change, unless the
criterion asks for them.
