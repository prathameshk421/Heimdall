package fixture

import "testing"

// TestRefund is skipped instead of running.
func TestRefund(t *testing.T) {
	// ruleid: go-no-skip
	t.Skip("the refund service is not ready")
}
