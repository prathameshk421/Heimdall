---
applies_to: []
check: "Does a write that a client may repeat give the same answer when it is repeated?"
proof: "Send the same request twice with the same key and show that the two answers are the same."
kind: convention
source: heimdall lens template: contract
status: active
---
A network call that times out leaves the client unsure whether the write happened. The client retries, and a write
that is not safe to repeat charges twice, sends twice or creates twice.

A write that a client may repeat needs a key the client controls, and the server must answer a repeat with the
answer it gave the first time. A server that answers a repeat with "already exists" pushes the problem to the
client and the client cannot tell it apart from a first attempt that failed.
