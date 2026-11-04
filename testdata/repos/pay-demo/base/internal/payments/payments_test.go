package payments

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)

// newTestService returns a service with a fixed clock and ids id_1, id_2, ...
func newTestService() (*Service, *MemoryStore) {
	store := NewMemoryStore()
	n := 0
	newID := func() string {
		n++
		return "id_" + strconv.Itoa(n)
	}
	return NewService(store, func() time.Time { return fixedNow }, newID), store
}

func TestValidateNewReportsEveryProblem(t *testing.T) {
	err := ValidateNew("", 0, "usd")
	if err == nil {
		t.Fatal("ValidateNew accepted an empty merchant, a zero amount and a lower-case currency")
	}
	for _, field := range []string{"merchant", "amount", "currency"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("error %q does not name %s", err, field)
		}
	}
}

func TestValidateNewAcceptsAValidPayment(t *testing.T) {
	if err := ValidateNew("m-1", 100, "USD"); err != nil {
		t.Fatalf("ValidateNew(valid) = %v", err)
	}
}

func TestCreateStoresThePaymentWithTheInjectedClock(t *testing.T) {
	svc, store := newTestService()
	p, err := svc.Create("m-1", 250, "EUR")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !p.CreatedAt.Equal(fixedNow) {
		t.Errorf("CreatedAt = %v, want the injected %v", p.CreatedAt, fixedNow)
	}
	got, ok := store.Get(p.ID)
	if !ok || got.Amount != 250 || got.MerchantID != "m-1" {
		t.Errorf("stored payment = %+v (found %v), want the created one", got, ok)
	}
}

func TestCreateRejectsAnInvalidRequestWithoutStoring(t *testing.T) {
	svc, store := newTestService()
	if _, err := svc.Create("m-1", -5, "USD"); err == nil {
		t.Fatal("Create accepted a negative amount")
	}
	if _, ok := store.Get("id_1"); ok {
		t.Error("an invalid payment was stored")
	}
}
