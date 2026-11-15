---
applies_to: []
check: "Does the name of the test say what behaviour it pins, so a failure says what broke?"
proof: "Read the name of the failing test and say what it means without opening the file."
kind: convention
source: heimdall lens template: tests
status: active
---
The name of a test is the only thing the person reading the failure sees first. A name that says "TestRefund_2" or
"test case 3" makes the reader open the file, and the file is the thing they were trying to avoid.

Name the test after the behaviour: what happens, under which condition. A table case needs a name of its own, so a
failure says which case failed and not only that a table failed.
