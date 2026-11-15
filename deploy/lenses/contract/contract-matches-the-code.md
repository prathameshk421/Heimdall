---
applies_to: ["**/openapi*.y*ml", "**/swagger*.json"]
check: "Do the status codes and the fields in the contract file match what the handler actually returns?"
proof: "Call the route and compare the answer with the file, field by field."
kind: convention
source: heimdall lens template: contract
status: active
---
The contract file is written by hand and the handler is written by hand. They drift, and the drift is found by a
consumer instead of by the review.

Read the route the change touches in the file and in the code. The status code, the body fields and the required
fields must be the same in both. A field the file marks as required and the handler leaves empty is a break that the
file announces on its own.
