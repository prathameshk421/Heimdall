---
applies_to: ["**/openapi*.y*ml", "**/*.proto", "**/swagger*.json"]
check: "Does a change that removes or narrows a field, a route or a value say that it breaks the contract?"
proof: "Run the contract diff on the base and on the head, and show the list of breaking changes."
kind: convention
source: heimdall lens template: contract
status: active
---
The contract is what the other programs were written against. A field that disappears, a route that moves, a value
that stops being accepted and a response that stops carrying a field are all breaks, even when the new code is
correct.

A break needs three things: the list of breaks, the version it belongs to, and what the consumers have to do. A
break that nobody was told about is an outage in someone else's deploy.
