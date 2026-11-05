package refunds

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/aarush323/heimdall-fixture-pay/internal/payments"
)

var testNow = time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)

// newFixture returns a service over one payment of 100 for merchant m-1.
func newFixture(t *testing.T) (*Service, string) {
	t.Helper()
	source := payments.NewMemoryStore()
	pay := payments.Payment{ID: "pay_1", MerchantID: "m-1", Amount: 100, Currency: "USD", CreatedAt: testNow}
	source.Put(pay)
	n := 0
	newID := func() string {
		n++
		return "ref_" + strconv.Itoa(n)
	}
	return NewService(source, func() time.Time { return testNow }, newID), pay.ID
}

func TestRefundOfTheWholePaymentIsAccepted(t *testing.T) {
	svc, payID := newFixture(t)
	r, replayed, err := svc.Create(Request{MerchantID: "m-1", PaymentID: payID, Amount: 100, IdempotencyKey: "k1"})
	if err != nil || replayed {
		t.Fatalf("Create(100 of 100) = replayed %v, err %v", replayed, err)
	}
	if r.Amount != 100 || r.Currency != "USD" || !r.CreatedAt.Equal(testNow) {
		t.Errorf("refund = %+v, want amount 100, currency USD and the injected time", r)
	}
}

func TestRefundLargerThanThePaymentIsRejected(t *testing.T) {
	svc, payID := newFixture(t)
	_, _, err := svc.Create(Request{MerchantID: "m-1", PaymentID: payID, Amount: 101, IdempotencyKey: "k1"})
	if !errors.Is(err, ErrExceedsPayment) {
		t.Fatalf("Create(101 of 100) err = %v, want ErrExceedsPayment", err)
	}
}

func TestRepeatedIdempotencyKeyReturnsTheOriginalRefund(t *testing.T) {
	svc, payID := newFixture(t)
	first, _, err := svc.Create(Request{MerchantID: "m-1", PaymentID: payID, Amount: 40, IdempotencyKey: "k1"})
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	second, replayed, err := svc.Create(Request{MerchantID: "m-1", PaymentID: payID, Amount: 40, IdempotencyKey: "k1"})
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if !replayed || second.ID != first.ID {
		t.Errorf("second Create = %q replayed %v, want the original %q replayed", second.ID, replayed, first.ID)
	}
}

func TestOnlyThePaymentsMerchantCanRefundIt(t *testing.T) {
	svc, payID := newFixture(t)
	_, _, err := svc.Create(Request{MerchantID: "m-2", PaymentID: payID, Amount: 10, IdempotencyKey: "k1"})
	if !errors.Is(err, ErrPaymentNotFound) {
		t.Fatalf("another merchant's refund err = %v, want ErrPaymentNotFound", err)
	}
}

func TestGetReturnsTheRefund(t *testing.T) {
	svc, payID := newFixture(t)
	created, _, err := svc.Create(Request{MerchantID: "m-1", PaymentID: payID, Amount: 10, IdempotencyKey: "k1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := svc.Get("m-1", created.ID)
	if err != nil || got.ID != created.ID {
		t.Fatalf("Get(%q) = %+v, %v; want the created refund", created.ID, got, err)
	}
}

func TestGetOfAnUnknownRefundIsNotFound(t *testing.T) {
	svc, _ := newFixture(t)
	if _, err := svc.Get("m-1", "ref_nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(unknown) err = %v, want ErrNotFound", err)
	}
}

func TestInvalidRequestsAreRejectedAsInvalid(t *testing.T) {
	cases := map[string]Request{
		"no merchant": {PaymentID: "pay_1", Amount: 1, IdempotencyKey: "k"},
		"no payment":  {MerchantID: "m-1", Amount: 1, IdempotencyKey: "k"},
		"zero amount": {MerchantID: "m-1", PaymentID: "pay_1", IdempotencyKey: "k"},
		"no key":      {MerchantID: "m-1", PaymentID: "pay_1", Amount: 1},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			svc, _ := newFixture(t)
			if _, _, err := svc.Create(req); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Create err = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestSeveralPartialRefundsCannotExceedThePayment(t *testing.T) {
	svc, payID := newFixture(t)
	if _, _, err := svc.Create(Request{MerchantID: "m-1", PaymentID: payID, Amount: 60, IdempotencyKey: "k1"}); err != nil {
		t.Fatalf("first refund of 60: %v", err)
	}
	_, _, err := svc.Create(Request{MerchantID: "m-1", PaymentID: payID, Amount: 60, IdempotencyKey: "k2"})
	if !errors.Is(err, ErrExceedsPayment) {
		t.Fatalf("second refund of 60 on a payment of 100 err = %v, want ErrExceedsPayment", err)
	}
}

func TestAnotherMerchantCannotReadARefund(t *testing.T) {
	svc, payID := newFixture(t)
	created, _, err := svc.Create(Request{MerchantID: "m-1", PaymentID: payID, Amount: 10, IdempotencyKey: "k1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Get("m-2", created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get by m-2 err = %v, want ErrNotFound", err)
	}
}
