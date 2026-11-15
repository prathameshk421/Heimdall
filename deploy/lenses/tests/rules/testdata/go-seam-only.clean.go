package fixture

import (
	"testing"

	"example.com/pay/payments"
)

// TestRefund calls the product over its seam.
func TestRefund(t *testing.T) {
	t.Log(payments.Refund)
}
