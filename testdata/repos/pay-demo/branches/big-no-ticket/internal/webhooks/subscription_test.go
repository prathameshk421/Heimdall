package webhooks

import (
	"errors"
	"strings"
	"testing"
)

func TestNewSubscriptionAcceptsHTTPSAndLocalhost(t *testing.T) {
	for _, u := range []string{"https://merchant.example/hooks", "http://localhost:9000/hooks", "http://127.0.0.1/hooks"} {
		if _, err := NewSubscription("sub_1", "m-1", u, []EventType{PaymentCreated}, secret, t0); err != nil {
			t.Errorf("NewSubscription(%s) = %v", u, err)
		}
	}
}

func TestNewSubscriptionReportsEveryProblemAtOnce(t *testing.T) {
	_, err := NewSubscription("sub_1", "m-1", "http://merchant.example/hooks", []EventType{"order.paid"}, []byte("short"), t0)
	if !errors.Is(err, ErrInvalidSubscription) {
		t.Fatalf("err = %v, want ErrInvalidSubscription", err)
	}
	for _, field := range []string{"url", "types", "secret"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("error does not name %s: %v", field, err)
		}
	}
}

func TestWantsOnlyActiveSubscriptionsOfTheRequestedType(t *testing.T) {
	sub, err := NewSubscription("sub_1", "m-1", "https://merchant.example/hooks", []EventType{RefundCreated}, secret, t0)
	if err != nil {
		t.Fatal(err)
	}
	if !sub.Wants(RefundCreated) || sub.Wants(PaymentCreated) {
		t.Errorf("Wants: refund %v payment %v, want true and false", sub.Wants(RefundCreated), sub.Wants(PaymentCreated))
	}
	sub.Active = false
	if sub.Wants(RefundCreated) {
		t.Error("an inactive subscription still wants events")
	}
}

func TestNewSubscriptionRefusesURLsThatCannotBeSafelyPosted(t *testing.T) {
	cases := map[string]string{
		"remote plain http": "http://merchant.example/hooks",
		"no host":           "https:///hooks",
		"ftp scheme":        "ftp://merchant.example/hooks",
		"unparsable":        "https://[::1",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NewSubscription("sub_1", "m-1", raw, []EventType{PaymentCreated}, secret, t0)
			if !errors.Is(err, ErrInvalidSubscription) {
				t.Fatalf("NewSubscription(%q) err = %v, want ErrInvalidSubscription", raw, err)
			}
		})
	}
}

func TestNewSubscriptionCopiesItsInputs(t *testing.T) {
	types := []EventType{PaymentCreated}
	key := []byte("signing-key-for-tests")
	sub, err := NewSubscription("sub_1", "m-1", "https://merchant.example/hooks", types, key, t0)
	if err != nil {
		t.Fatal(err)
	}
	types[0] = RefundCreated
	key[0] = 'X'
	if sub.Types[0] != PaymentCreated || sub.Secret[0] != 's' {
		t.Errorf("subscription shares its inputs with the caller: types %v secret %q", sub.Types, sub.Secret)
	}
}
