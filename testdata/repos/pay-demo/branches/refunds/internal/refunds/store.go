package refunds

// idempotencyKey scopes a client's Idempotency-Key to the merchant that sent it, so one merchant cannot replay
// another merchant's refund.
type idempotencyKey struct {
	merchantID string
	key        string
}

// store keeps refunds and the keys that created them. It has no lock of its own: Service serialises every call,
// so the cap check and the write happen as one step.
type store struct {
	refunds map[string]Refund
	byKey   map[idempotencyKey]string
}

func newStore() *store {
	return &store{refunds: make(map[string]Refund), byKey: make(map[idempotencyKey]string)}
}

// put records r as created by key.
func (s *store) put(r Refund, key idempotencyKey) {
	s.refunds[r.ID] = r
	s.byKey[key] = r.ID
}

// get returns the refund with id.
func (s *store) get(id string) (Refund, bool) {
	r, ok := s.refunds[id]
	return r, ok
}

// byIdempotencyKey returns the refund that key created, if there is one.
func (s *store) byIdempotencyKey(key idempotencyKey) (Refund, bool) {
	id, ok := s.byKey[key]
	if !ok {
		return Refund{}, false
	}
	return s.get(id)
}

// refundedFor returns the total already refunded from paymentID.
func (s *store) refundedFor(paymentID string) int64 {
	var total int64
	for _, r := range s.refunds {
		if r.PaymentID == paymentID {
			total += r.Amount
		}
	}
	return total
}
