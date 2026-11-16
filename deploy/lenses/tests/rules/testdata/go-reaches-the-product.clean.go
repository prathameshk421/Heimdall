package fixture

import (
	"testing"

	"example.com/pay/payments"
)

// TestRefund reaches the product.
func TestRefund(t *testing.T) {
	t.Log(payments.Refund)
}
