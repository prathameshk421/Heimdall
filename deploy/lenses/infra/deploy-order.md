---
applies_to: ["**/deploy/**", "**/scripts/**", "**/*.sh", "**/.github/workflows/**"]
check: "Does the deploy run its steps in an order that keeps the running version working until the new one is ready?"
proof: "Write the steps of the deploy in order and show that the service works after each one of them."
kind: convention
source: heimdall lens template: infra
status: active
---
A deploy is a sequence, and the sequence is part of the change. The step that changes the schema has to run before
the code that needs it, and the step that removes the old shape has to run after the code that used it.

Read the deploy as the person who watches it at three in the morning. Which step is the point of no return, and what
is still serving while it runs?
