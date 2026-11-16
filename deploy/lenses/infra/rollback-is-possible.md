---
applies_to: ["**/deploy/**", "**/*.tf", "**/charts/**", "**/helm/**"]
check: "Can the previous version be put back, and does it still work with the data the new version wrote?"
proof: "Name the step that would be undone and show that it does not need the new code to be rolled back first."
kind: convention
source: heimdall lens template: infra
status: active
---
A rollback that only restores the code is not a rollback when the change also wrote data. The old code must still
understand what the new code wrote, or the rollback makes things worse than the failure it undoes.

Say what is undone and in which order, and check that the old version can read the new data. When it cannot, the
change needs a way forward that is not "put the old code back".
