---
applies_to: []
check: "Does every new test fail on the parent of the change, for the right reason, and not with an error?"
proof: "Run the new test on the parent commit and show the failure message it gives."
kind: convention
source: heimdall lens template: tests
status: active
---
A test that passes before the change proves nothing about the change. A test that fails with a compile error or a
missing route proves nothing either: it fails for a reason that has nothing to do with the behaviour.

The test has to fail on the parent with an assertion that names the behaviour. A route that does not exist yet gives
a 404, and asserting the 404 that the feature will replace is a correct red.
