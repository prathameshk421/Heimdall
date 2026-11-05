package webhooks

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// Status is the outcome of one event's delivery to one subscription.
type Status string

// The delivery outcomes.
const (
	Delivered Status = "delivered"
	Failed    Status = "failed"
)

// ErrNotFound means no active subscription with that id exists for the merchant.
var ErrNotFound = errors.New("subscription not found")

// Delivery records what happened when one event was sent to one subscription.
type Delivery struct {
	EventID        string
	SubscriptionID string
	MerchantID     string
	Status         Status
	Attempts       int
	LastStatusCode int
	LastError      string
}

// Store keeps subscriptions and delivery records in memory. It is safe for concurrent use.
type Store struct {
	mu            sync.Mutex
	subscriptions map[string]Subscription
	deliveries    []Delivery
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{subscriptions: make(map[string]Subscription)}
}

// Add stores a new subscription. An id that is already in use is an error, never an overwrite.
func (s *Store) Add(sub Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, taken := s.subscriptions[sub.ID]; taken {
		return fmt.Errorf("subscription %s: id is already in use", sub.ID)
	}
	s.subscriptions[sub.ID] = sub
	return nil
}

// Deactivate stops deliveries to the merchant's subscription id. Another merchant's id is ErrNotFound.
func (s *Store) Deactivate(merchantID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subscriptions[id]
	if !ok || sub.MerchantID != merchantID {
		return ErrNotFound
	}
	sub.Active = false
	s.subscriptions[id] = sub
	return nil
}

// Subscriptions lists the merchant's subscriptions, active or not, by id.
func (s *Store) Subscriptions(merchantID string) []Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Subscription
	for _, sub := range s.subscriptions {
		if sub.MerchantID == merchantID {
			out = append(out, sub)
		}
	}
	slices.SortFunc(out, func(a, b Subscription) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// Matching lists the active subscriptions of the event's merchant that asked for its type, by id.
func (s *Store) Matching(e Event) []Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Subscription
	for _, sub := range s.subscriptions {
		if sub.MerchantID == e.MerchantID && sub.Wants(e.Type) {
			out = append(out, sub)
		}
	}
	slices.SortFunc(out, func(a, b Subscription) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// Record appends one delivery outcome.
func (s *Store) Record(d Delivery) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deliveries = append(s.deliveries, d)
}

// Deliveries lists the merchant's delivery outcomes in the order they were recorded.
func (s *Store) Deliveries(merchantID string) []Delivery {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Delivery
	for _, d := range s.deliveries {
		if d.MerchantID == merchantID {
			out = append(out, d)
		}
	}
	return out
}
