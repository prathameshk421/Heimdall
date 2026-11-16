---
applies_to: []
check: "Does the body of an error keep its shape, so a client that reads it does not break?"
proof: "Show the error body before the change and after it, and show what a client that reads the old fields sees."
kind: convention
source: heimdall lens template: contract
status: active
---
Clients read errors. They read the code, the message and the fields, and they branch on them. An error body that
changes shape breaks the clients that read it, usually in the path that handles the failure, where nobody is looking.

Keep the fields of the error body, keep the code, and add new ones. When an error body must change, it changes with
a version, and the old shape keeps working until the clients have moved.
