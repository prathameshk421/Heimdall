---
applies_to: []
check: "Does every wait carry a deadline, and does no code wait on the clock with a fixed sleep?"
proof: "Show the call the request waits on and the deadline it carries."
kind: convention
source: heimdall lens template: operability
status: active
---
A call with no timeout waits until the other side gives up, which may be never. The connection stays open, the
worker stays busy, and the requests behind it queue up until the service is out of workers.

Every call out of the process carries a deadline, and the deadline is shorter than the deadline of the request that
made it. A fixed sleep is the same problem with a number written next to it: it is a guess about how long the other
side needs, and it is wrong exactly when the system is busy.
