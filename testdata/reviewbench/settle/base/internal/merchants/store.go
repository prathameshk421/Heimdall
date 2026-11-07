package merchants

import (
	"errors"
	"sync"
)

// ErrNotFound says the merchant does not exist.
var ErrNotFound = errors.New("merchant not found")

// Merchant is one account we pay out to.
type Merchant struct {
	ID      string
	IBAN    string
	Balance int64
	Frozen  bool
}

// Store holds merchants in memory.
type Store struct {
	mu   sync.Mutex
	byID map[string]*Merchant
}

// NewStore returns an empty store.
func NewStore() *Store { return &Store{byID: map[string]*Merchant{}} }

// Lookup returns the merchant, or ErrNotFound.
func (s *Store) Lookup(id string) (*Merchant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	return m, nil
}

// Due lists the ids with a positive balance.
func (s *Store) Due() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for id, m := range s.byID {
		if m.Balance > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}
