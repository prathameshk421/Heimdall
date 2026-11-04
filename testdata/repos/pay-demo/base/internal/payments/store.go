package payments

import "sync"

// MemoryStore keeps payments in memory. It is safe for concurrent use.
type MemoryStore struct {
	mu       sync.Mutex
	payments map[string]Payment
}

// NewMemoryStore returns an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{payments: make(map[string]Payment)}
}

// Put records p under its ID, replacing any payment with that ID.
func (s *MemoryStore) Put(p Payment) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.payments[p.ID] = p
}

// Get returns the payment with id and whether it exists.
func (s *MemoryStore) Get(id string) (Payment, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.payments[id]
	return p, ok
}
