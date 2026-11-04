package webhooks

import (
	"errors"
	"strconv"
	"testing"
	"time"
)

// newTestService returns a service with a fixed clock and ids sub_1, sub_2, ...
func newTestService() *Service {
	n := 0
	newID := func() string {
		n++
		return "sub_" + strconv.Itoa(n)
	}
	return NewService(NewStore(), func() time.Time { return t0 }, newID)
}

func TestSubscriptionsAreScopedToTheMerchant(t *testing.T) {
	svc := newTestService()
	if _, err := svc.Subscribe("m-1", "https://a.example/hooks", []EventType{PaymentCreated}, secret); err != nil {
		t.Fatal(err)
	}
	if got := svc.Subscriptions("m-2"); len(got) != 0 {
		t.Errorf("m-2 sees %d subscriptions of m-1", len(got))
	}
	if err := svc.Unsubscribe("m-2", "sub_1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("m-2 deactivating m-1's subscription err = %v, want ErrNotFound", err)
	}
	if err := svc.Unsubscribe("m-1", "sub_1"); err != nil {
		t.Errorf("m-1 deactivating its own subscription: %v", err)
	}
}

func TestMatchingNeedsTheMerchantAndTheType(t *testing.T) {
	store := NewStore()
	mine, err := NewSubscription("sub_1", "m-1", "https://a.example/hooks", []EventType{PaymentCreated}, secret, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add(mine); err != nil {
		t.Fatal(err)
	}
	if got := store.Matching(Event{ID: "e", Type: PaymentCreated, MerchantID: "m-1"}); len(got) != 1 {
		t.Errorf("Matching(own payment) = %d subscriptions, want 1", len(got))
	}
	if got := store.Matching(Event{ID: "e", Type: RefundCreated, MerchantID: "m-1"}); len(got) != 0 {
		t.Errorf("Matching(refund) = %d subscriptions, want 0", len(got))
	}
	if got := store.Matching(Event{ID: "e", Type: PaymentCreated, MerchantID: "m-2"}); len(got) != 0 {
		t.Errorf("Matching(other merchant) = %d subscriptions, want 0", len(got))
	}
}

func TestStoreRefusesAnIDTwice(t *testing.T) {
	store := NewStore()
	sub, err := NewSubscription("sub_1", "m-1", "https://a.example/hooks", []EventType{PaymentCreated}, secret, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add(sub); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(sub); err == nil {
		t.Error("Add overwrote an existing subscription id")
	}
}

func TestDeactivatedSubscriptionsStopMatching(t *testing.T) {
	svc := newTestService()
	sub, err := svc.Subscribe("m-1", "https://a.example/hooks", []EventType{PaymentCreated}, secret)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Unsubscribe("m-1", sub.ID); err != nil {
		t.Fatal(err)
	}
	if got := svc.store.Matching(Event{ID: "e", Type: PaymentCreated, MerchantID: "m-1"}); len(got) != 0 {
		t.Errorf("Matching after deactivation = %d subscriptions, want 0", len(got))
	}
	listed := svc.Subscriptions("m-1")
	if len(listed) != 1 || listed[0].Active {
		t.Errorf("listing after deactivation = %+v, want the one subscription, inactive", listed)
	}
}

func TestDeliveriesAreListedInRecordOrderPerMerchant(t *testing.T) {
	store := NewStore()
	store.Record(Delivery{EventID: "evt_1", SubscriptionID: "sub_1", MerchantID: "m-1", Status: Delivered, Attempts: 1})
	store.Record(Delivery{EventID: "evt_2", SubscriptionID: "sub_9", MerchantID: "m-2", Status: Failed, Attempts: 3})
	store.Record(Delivery{EventID: "evt_3", SubscriptionID: "sub_1", MerchantID: "m-1", Status: Failed, Attempts: 3})
	got := store.Deliveries("m-1")
	if len(got) != 2 || got[0].EventID != "evt_1" || got[1].EventID != "evt_3" {
		t.Errorf("Deliveries(m-1) = %+v, want evt_1 then evt_3", got)
	}
}
