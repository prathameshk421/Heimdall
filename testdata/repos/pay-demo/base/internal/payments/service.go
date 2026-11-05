package payments

import (
	"fmt"
	"time"
)

// Service creates payments. Now and newID are injected, so tests need no real clock or randomness.
type Service struct {
	store *MemoryStore
	now   func() time.Time
	newID func() string
}

// NewService returns a Service that records payments in store.
func NewService(store *MemoryStore, now func() time.Time, newID func() string) *Service {
	return &Service{store: store, now: now, newID: newID}
}

// Create validates the request, records the payment for merchantID and returns it.
func (s *Service) Create(merchantID string, amount int64, currency string) (Payment, error) {
	if err := ValidateNew(merchantID, amount, currency); err != nil {
		return Payment{}, fmt.Errorf("create payment: %w", err)
	}
	p := Payment{
		ID:         s.newID(),
		MerchantID: merchantID,
		Amount:     amount,
		Currency:   currency,
		CreatedAt:  s.now().UTC(),
	}
	s.store.Put(p)
	return p, nil
}
