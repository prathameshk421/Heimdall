---
applies_to: []
check: "Does the change have a signal that shows it is failing, so a broken deploy is seen before a customer reports it?"
proof: "Name the metric or the log line that fires when the new path fails."
kind: convention
source: heimdall lens template: operability
status: active
---
A change that fails quietly fails for hours. The dashboard shows nothing because the code never reached the part
that counts, and the first sign of it is a customer saying it did not work.

Every new path has a signal: a counter of the failures, a log line at the error level, a timing. When the path can
fail in a way the caller cannot see, the signal is the only way it is noticed.
