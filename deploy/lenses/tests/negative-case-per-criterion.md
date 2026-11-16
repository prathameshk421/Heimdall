---
applies_to: []
check: "Does every criterion have a case that must fail, and not only the case where everything works?"
proof: "Map each criterion to the tests that cover it and name the negative one."
kind: convention
source: heimdall lens template: tests
status: active
---
The happy path passes on the day the code is written, and it passes on the day the code is wrong. A criterion with
only a happy path is a criterion nobody has tried to break.

Every criterion needs a case where the answer is wrong: another owner's object, an amount that is too large, a
missing field, a request that is not allowed. A criterion with no negative case is a criterion that has not been
tested.
