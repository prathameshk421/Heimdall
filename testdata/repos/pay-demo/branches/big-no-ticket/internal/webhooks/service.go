package webhooks

import "time"

// Service manages a merchant's subscriptions and reads back their deliveries. Every call is scoped to one merchant.
type Service struct {
	store *Store
	now   func() time.Time
	newID func() string
}

// NewService returns a Service over store. now and newID are injected for tests.
func NewService(store *Store, now func() time.Time, newID func() string) *Service {
	return &Service{store: store, now: now, newID: newID}
}

// Subscribe validates and stores a new active subscription for merchantID.
func (s *Service) Subscribe(merchantID, rawURL string, types []EventType, secret []byte) (Subscription, error) {
	sub, err := NewSubscription(s.newID(), merchantID, rawURL, types, secret, s.now())
	if err != nil {
		return Subscription{}, err
	}
	if err := s.store.Add(sub); err != nil {
		return Subscription{}, err
	}
	return sub, nil
}

// Unsubscribe deactivates the merchant's subscription id.
func (s *Service) Unsubscribe(merchantID, id string) error {
	return s.store.Deactivate(merchantID, id)
}

// Subscriptions lists the merchant's subscriptions.
func (s *Service) Subscriptions(merchantID string) []Subscription {
	return s.store.Subscriptions(merchantID)
}

// Deliveries lists the merchant's delivery outcomes.
func (s *Service) Deliveries(merchantID string) []Delivery {
	return s.store.Deliveries(merchantID)
}
