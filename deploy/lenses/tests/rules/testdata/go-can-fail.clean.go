package fixture

import "testing"

// TestRefund asserts the outcome it is about.
func TestRefund(t *testing.T) {
	if got := refund(); got != 100 {
		t.Fatalf("the refund was %d", got)
	}
}
