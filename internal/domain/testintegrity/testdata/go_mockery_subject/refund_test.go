package pay_test

import (
	"testing"

	"github.com/acme/shop/internal/pay"
	"github.com/acme/shop/internal/pay/mocks"
)

func TestRefundUsesLedger(t *testing.T) {
	ledger := mocks.NewLedger(t)
	ledger.EXPECT().Record("ch_1", 500).Return(nil)
	if err := pay.Refund(ledger, "ch_1", 500); err != nil {
		t.Fatal(err)
	}
}
