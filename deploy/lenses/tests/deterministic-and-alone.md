---
applies_to: []
check: "Is the test deterministic and alone: no sleep, no clock, no shared state and no network?"
proof: "Run the test twice in a row and show that it gives the same answer both times."
kind: convention
source: heimdall lens template: tests
status: active
---
A test that waits for a fixed time passes on a fast machine and fails on a slow one. A test that reads the clock
passes today and fails tomorrow. A test that shares state with another test passes alone and fails in the suite.

Wait for the thing, not for the time. Give the test its own data. Let the harness set the seed and read it, so the
same run is the same run.
