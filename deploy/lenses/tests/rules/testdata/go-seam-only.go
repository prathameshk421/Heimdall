package fixture

import (
	"testing"

	// ruleid: go-seam-only
	"example.com/pay/internal/payments"
)

// TestRefund imports the inside of the product.
func TestRefund(t *testing.T) {
	t.Log(payments.Refund)
}
