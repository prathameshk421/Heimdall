---
applies_to: []
check: "Does the change handle the empty, the zero and the largest value the callers can pass?"
proof: "Add one case with an empty collection and one with the largest value the API allows."
kind: convention
source: heimdall lens template: correctness
status: active
---
The input that breaks code is rarely the input in the ticket. It is the empty list, the zero, the maximum the API
accepts, or the value one past it.

For each value the change reads, name the boundary the callers can reach and check the code at the boundary. A loop
over a collection that may be empty, a division that may divide by zero, and a page that may ask for a page past the
last one are the three that pay most often.
