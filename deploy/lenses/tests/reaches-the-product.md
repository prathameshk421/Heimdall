---
applies_to: []
check: "Does the test reach the product through the declared seam, and not by importing what it is testing?"
proof: "Show the imports of the test file and the seam that the task declared."
kind: convention
source: heimdall lens template: tests
status: active
---
A test that imports the internals of the product is coupled to its shape, so it breaks on every refactor and says
nothing about the behaviour. A test that imports nothing of the product cannot fail for a real reason at all.

The test calls the product the way a caller does: over the seam the profile declares. It never imports a package the
product keeps to itself.
