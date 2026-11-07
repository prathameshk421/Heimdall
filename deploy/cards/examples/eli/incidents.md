# Incidents

- INC-031: a refund change removed `refund_ref`. The ledger could not parse responses for about forty minutes and the daily balance was off. Lesson: a wire field the consumer reads stays until the consumer has moved.
