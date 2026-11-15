---
applies_to: []
check: "Does a write and the record that announces it happen together, so a reader never sees one without the other?"
proof: "Fail the write after the record was written, and show that neither of the two is visible."
kind: convention
source: heimdall lens template: data
status: active
---
A row and the event that says it happened are two writes. When they are not in one transaction, a crash between them
leaves a reader with one and not the other: the money moved and no record says so, or the record says the money moved
and it did not.

Put the write and the record in the same transaction, or make the record derivable from the write. An outbox row
written in the same transaction and delivered later is the usual answer.
